package saml

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildbuddy-io/buildbuddy/enterprise/server/auth"
	"github.com/buildbuddy-io/buildbuddy/server/environment"
	"github.com/buildbuddy-io/buildbuddy/server/interfaces"
	"github.com/buildbuddy-io/buildbuddy/server/tables"
	"github.com/buildbuddy-io/buildbuddy/server/util/status"
	"github.com/crewjam/saml/samlsp"
)

const (
	authRedirectParam      = "redirect_url"
	slugParam              = "slug"
	slugCookie             = "Slug"
	cookieDuration         = 365 * 24 * time.Hour
	contextSamlSessionKey  = "saml.session"
	contextSamlEntityIDKey = "saml.entityID"
)

var (
	samlFirstNameAttributes = []string{"FirstName", "givenName", "givenname", "given_name"}
	samlLastNameAttributes  = []string{"LastName", "surname", "sn"}
	samlEmailAttributes     = []string{"email", "mail", "emailAddress", "Email", "emailaddress", "email_address"}
	samlSubjectAttributes   = append([]string{"urn:oasis:names:tc:SAML:attribute:subject-id", "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent", "user_id", "username"}, samlEmailAttributes...)
)

type SAMLAuthenticator struct {
	env           environment.Env
	samlProviders map[string]*samlsp.Middleware
}

func NewSAMLAuthenticator(env environment.Env) *SAMLAuthenticator {
	return &SAMLAuthenticator{
		env:           env,
		samlProviders: make(map[string]*samlsp.Middleware),
	}
}

func (a *SAMLAuthenticator) Login(w http.ResponseWriter, r *http.Request) error {
	slug := r.URL.Query().Get(slugParam)
	if slug == "" {
		return status.FailedPreconditionError("SAML Login attempted without a slug")
	}
	sp, err := a.serviceProviderFromRequest(r)
	if err != nil {
		return err
	}
	auth.SetCookie(w, slugCookie, slug, time.Now().Add(cookieDuration))
	session, err := sp.Session.GetSession(r)
	if session != nil {
		redirectURL := r.URL.Query().Get(authRedirectParam)
		if redirectURL == "" {
			redirectURL = "/" // default to redirecting home.
		}
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
		return nil
	}
	sp.HandleStartAuthFlow(w, r)
	return nil
}

func (a *SAMLAuthenticator) AuthenticatedHTTPContext(w http.ResponseWriter, r *http.Request) context.Context {
	ctx := r.Context()
	if sp, err := a.serviceProviderFromRequest(r); err == nil {
		session, _ := sp.Session.GetSession(r)
		sa, ok := session.(samlsp.SessionWithAttributes)
		if ok {
			ctx = context.WithValue(ctx, contextSamlSessionKey, sa)
			ctx = context.WithValue(ctx, contextSamlEntityIDKey, sp.ServiceProvider.EntityID)
			return ctx
		}
	}
	return ctx
}

func (a *SAMLAuthenticator) FillUser(ctx context.Context, user *tables.User) error {
	pk, err := tables.PrimaryKeyForTable("Users")
	if err != nil {
		return err
	}
	if subjectID, session := a.subjectIDAndSessionFromContext(ctx); subjectID != "" && session != nil {
		attributes := session.GetAttributes()
		user.UserID = pk
		user.SubID = subjectID
		user.FirstName = firstSet(attributes, samlFirstNameAttributes)
		user.LastName = firstSet(attributes, samlLastNameAttributes)
		user.Email = firstSet(attributes, samlEmailAttributes)
		return nil
	}
	return status.UnauthenticatedError("No SAML User found")
}

func (a *SAMLAuthenticator) Logout(w http.ResponseWriter, r *http.Request) error {
	auth.ClearCookie(w, slugCookie)
	if sp, err := a.serviceProviderFromRequest(r); err == nil {
		sp.Session.DeleteSession(w, r)
	}
	return status.UnauthenticatedError("Logged out!")
}

func (a *SAMLAuthenticator) AuthenticatedUser(ctx context.Context) (interfaces.UserInfo, error) {
	if s, _ := a.subjectIDAndSessionFromContext(ctx); s != "" {
		claims, err := auth.ClaimsFromSubID(a.env, ctx, s)
		return claims, err
	}
	return nil, status.UnauthenticatedError("No SAML User found")
}

func (a *SAMLAuthenticator) Auth(w http.ResponseWriter, r *http.Request) error {
	if !strings.HasPrefix(r.URL.Path, "/auth/saml/") {
		return status.NotFoundErrorf("Auth path does not have SAML prefix %s:", r.URL.Path)
	}
	sp, err := a.serviceProviderFromRequest(r)
	if err != nil {
		return status.NotFoundErrorf("SAML Auth Failed: %s", err)
	}
	sp.ServeHTTP(w, r)
	return nil
}

func (a *SAMLAuthenticator) serviceProviderFromRequest(r *http.Request) (*samlsp.Middleware, error) {
	slug := a.getSlugFromRequest(r)
	if slug == "" {
		return nil, status.FailedPreconditionError("Organization slug not set")
	}
	provider, ok := a.samlProviders[slug]
	if ok {
		return provider, nil
	}
	SAMLConfig := a.env.GetConfigurator().GetSAMLConfig()
	if SAMLConfig.CertFile == "" || SAMLConfig.KeyFile == "" {
		return nil, status.UnauthenticatedError("No SAML Configured")
	}
	keyPair, err := tls.LoadX509KeyPair(SAMLConfig.CertFile, SAMLConfig.KeyFile)
	if err != nil {
		return nil, err
	}
	keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, err
	}
	idpMetadataURL, err := a.getSAMLMetadataUrlForSlug(r.Context(), slug)
	if err != nil {
		return nil, err
	}
	idpMetadata, err := samlsp.FetchMetadata(context.Background(), http.DefaultClient,
		*idpMetadataURL)
	if err != nil {
		return nil, err
	}
	myURL, err := url.Parse(a.env.GetConfigurator().GetAppBuildBuddyURL())
	if err != nil {
		return nil, err
	}
	authURL, err := myURL.Parse("/auth/")
	if err != nil {
		return nil, err
	}
	entityURL, err := myURL.Parse("saml/metadata")
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("%s=%s", slugParam, slug)
	entityURL.RawQuery = query
	samlSP, _ := samlsp.New(samlsp.Options{
		EntityID:    entityURL.String(),
		URL:         *authURL,
		Key:         keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate: keyPair.Leaf,
		IDPMetadata: idpMetadata,
	})
	samlSP.ServiceProvider.MetadataURL.RawQuery = query
	samlSP.ServiceProvider.AcsURL.RawQuery = query
	samlSP.ServiceProvider.SloURL.RawQuery = query
	a.samlProviders[slug] = samlSP
	return samlSP, nil
}

func (a *SAMLAuthenticator) getSlugFromRequest(r *http.Request) string {
	slug := r.URL.Query().Get(slugParam)
	if slug == "" {
		slug = auth.GetCookie(r, slugCookie)
	}
	return slug
}

func (a *SAMLAuthenticator) getSAMLMetadataUrlForSlug(ctx context.Context, slug string) (*url.URL, error) {
	group, err := a.groupForSlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if group.SamlIdpMetadataUrl == "" {
		return nil, status.UnauthenticatedErrorf("Group %s does not have SAML configured", slug)
	}
	metadataUrl, err := url.Parse(group.SamlIdpMetadataUrl)
	if err != nil {
		return nil, err
	}
	return metadataUrl, nil
}

func (a *SAMLAuthenticator) groupForSlug(ctx context.Context, slug string) (*tables.Group, error) {
	userDB := a.env.GetUserDB()
	if userDB == nil {
		return nil, status.UnauthenticatedErrorf("UserDB not found")
	}
	if slug == "" {
		return nil, status.UnauthenticatedErrorf("Slug is required.")
	}
	return userDB.GetGroupByURLIdentifier(ctx, slug)
}

func (a *SAMLAuthenticator) subjectIDAndSessionFromContext(ctx context.Context) (string, samlsp.SessionWithAttributes) {
	entityID, ok := ctx.Value(contextSamlEntityIDKey).(string)
	if !ok || entityID == "" {
		return "", nil
	}
	if sa, ok := ctx.Value(contextSamlSessionKey).(samlsp.SessionWithAttributes); ok {
		return fmt.Sprintf("%s/%s", entityID, firstSet(sa.GetAttributes(), samlSubjectAttributes)), sa
	}
	return "", nil
}

func firstSet(attributes samlsp.Attributes, keys []string) string {
	for _, key := range keys {
		value := attributes.Get(key)
		if value == "" {
			continue
		}
		return value
	}
	return ""
}
