package tracing

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buildbuddy-io/buildbuddy/server/config"
	"github.com/buildbuddy-io/buildbuddy/server/interfaces"
	"github.com/buildbuddy-io/buildbuddy/server/util/log"
	"github.com/buildbuddy-io/buildbuddy/server/util/status"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"

	tpb "github.com/buildbuddy-io/buildbuddy/proto/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const (
	resourceDetectionTimeout      = 5 * time.Second
	traceHeader                   = "x-buildbuddy-trace"
	forceTraceHeaderValue         = "force"
	buildBuddyInstrumentationName = "buildbuddy.io"
)

// fractionSampler allows specifying a default sampling fraction as well as overrides based on the span name.
// Based on TraceIDRatioBased sampler from the OpenTelemetry library.
type fractionSampler struct {
	traceIDUpperBound          uint64
	traceIDUpperBoundOverrides map[string]uint64
	description                string
	ignoreForcedTracingHeader  bool
}

func newFractionSampler(fraction float64, fractionOverrides map[string]float64, ignoreForcedTracingHeader bool) *fractionSampler {
	configDescription := fmt.Sprintf("default=%f", fraction)
	boundOverrides := make(map[string]uint64)
	for n, f := range fractionOverrides {
		boundOverrides[n] = uint64(f * math.MaxInt64)
		configDescription += fmt.Sprintf(",%s=%f", n, f)
	}

	return &fractionSampler{
		traceIDUpperBound:          uint64(fraction * math.MaxInt64),
		traceIDUpperBoundOverrides: boundOverrides,
		description:                fmt.Sprintf("FractionSampler(%s)", configDescription),
		ignoreForcedTracingHeader:  ignoreForcedTracingHeader,
	}
}

func (s *fractionSampler) checkForcedTrace(parameters sdktrace.SamplingParameters) bool {
	if s.ignoreForcedTracingHeader {
		return false
	}
	md, ok := metadata.FromIncomingContext(parameters.ParentContext)
	if !ok {
		// Not an incoming gRPC request
		return false
	}
	hdrs, ok := md[traceHeader]
	return ok && len(hdrs) > 0 && hdrs[0] == forceTraceHeaderValue
}

func (s *fractionSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	psc := trace.SpanContextFromContext(parameters.ParentContext)

	// Check if sampling is forced via gRPC header.
	if s.checkForcedTrace(parameters) {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Attributes: parameters.Attributes,
			Tracestate: psc.TraceState(),
		}
	}

	bound := s.traceIDUpperBound
	if boundOverride, ok := s.traceIDUpperBoundOverrides[parameters.Name]; ok {
		bound = boundOverride
	}

	x := binary.BigEndian.Uint64(parameters.TraceID[0:8]) >> 1
	decision := sdktrace.Drop
	if x < bound {
		decision = sdktrace.RecordAndSample
	}
	return sdktrace.SamplingResult{
		Decision:   decision,
		Attributes: parameters.Attributes,
		Tracestate: psc.TraceState(),
	}
}

func (s *fractionSampler) Description() string {
	return s.description
}

func Configure(configurator *config.Configurator, healthChecker interfaces.HealthChecker) error {
	if configurator.GetTraceFraction() <= 0 {
		return nil
	}

	collector := configurator.GetTraceJaegerCollector()
	if collector == "" {
		return status.InvalidArgumentErrorf("Tracing enabled but Jaeger collector endpoint is not set.")
	}

	traceExporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(collector)))
	if err != nil {
		log.Warningf("Could not initialize Cloud Trace exporter: %s", err)
		return nil
	}

	fractionOverrides := make(map[string]float64)
	for _, override := range configurator.GetTraceFractionOverrides() {
		parts := strings.Split(override, "=")
		if len(parts) != 2 {
			return status.InvalidArgumentErrorf("Trace fraction override %q has invalid format, expected name=fraction", override)
		}
		name := parts[0]
		fraction, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return status.InvalidArgumentErrorf("Trace fraction override %q has invalid format, fraction not valid: %s", override, err)
		}
		fractionOverrides[name] = fraction
	}
	sampler := newFractionSampler(configurator.GetTraceFraction(), fractionOverrides, configurator.GetIgnoreForcedTracingHeader())

	var resourceAttrs []attribute.KeyValue
	if configurator.GetTraceServiceName() != "" {
		resourceAttrs = append(resourceAttrs, semconv.ServiceNameKey.String(configurator.GetTraceServiceName()))
	}

	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	healthChecker.RegisterShutdownFunction(func(ctx context.Context) error {
		return bsp.Shutdown(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), resourceDetectionTimeout)
	defer cancel()
	res, err := resource.New(ctx,
		resource.WithDetectors(&gcp.GKE{}, &gcp.GCE{}),
		resource.WithAttributes(resourceAttrs...))
	if err != nil {
		log.Warningf("Could not automatically detect resource information for tracing: %s", err)
		res = resource.NewSchemaless(resourceAttrs...)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithSampler(sdktrace.ParentBased(sampler)),
		sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	// Re-enable this if GCS tracing is fixed to not include blob names in span names
	// Necessary imports: "go.opentelemetry.io/otel/bridge/opencensus"
	//                    octrace "go.opencensus.io/trace"
	//
	// tracer := otel.GetTracerProvider().Tracer(buildBuddyInstrumentationName)
	// octrace.DefaultTracer = opencensus.NewTracer(tracer)
	propagator := propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})
	otel.SetTextMapPropagator(propagator)
	log.Infof("Tracing enabled with sampler: %s", sampler.Description())
	return nil
}

type SetMetadata func(m *tpb.Metadata)

type traceMetadataProtoCarrier struct {
	metadata    map[string]string
	setMetadata SetMetadata
}

func newTraceMetadataProtoCarrier(metadata *tpb.Metadata, setMetadata SetMetadata) *traceMetadataProtoCarrier {
	return &traceMetadataProtoCarrier{
		metadata:    metadata.GetEntries(),
		setMetadata: setMetadata,
	}
}

func (c *traceMetadataProtoCarrier) Get(key string) string {
	return c.metadata[key]
}

func (c *traceMetadataProtoCarrier) Set(key string, value string) {
	if c.metadata == nil {
		c.metadata = make(map[string]string)
		if c.setMetadata == nil {
			// Should never happen since this is only called via Inject which always sets setMetadata function.
			log.Errorf("Can't set metadata w/o setMetadata function")
			return
		}
		c.setMetadata(&tpb.Metadata{Entries: c.metadata})
	}
	c.metadata[key] = value
}

func (c *traceMetadataProtoCarrier) Keys() []string {
	var keys []string
	for k := range c.metadata {
		keys = append(keys, k)
	}
	return keys
}

func InjectProtoTraceMetadata(ctx context.Context, metadata *tpb.Metadata, setMetadata SetMetadata) {
	p := otel.GetTextMapPropagator()
	p.Inject(ctx, newTraceMetadataProtoCarrier(metadata, setMetadata))
}

func ExtractProtoTraceMetadata(ctx context.Context, metadata *tpb.Metadata) context.Context {
	p := otel.GetTextMapPropagator()
	return p.Extract(ctx, newTraceMetadataProtoCarrier(metadata, nil))
}

type HttpServeMux struct {
	delegateMux    *http.ServeMux
	tracingHandler http.Handler
}

func NewHttpServeMux(delegate *http.ServeMux) *HttpServeMux {
	// By default otelhttp names all the spans with the passed operation name.
	// We pass an empty name here since we override the span name in http handler.
	handler := otelhttp.NewHandler(delegate, "" /* operation= */)
	return &HttpServeMux{delegateMux: delegate, tracingHandler: handler}
}

func (m *HttpServeMux) Handle(pattern string, handler http.Handler) {
	m.delegateMux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		span.SetName(fmt.Sprintf("%s %s", r.Method, pattern))
		handler.ServeHTTP(w, r)
	})
}

func (m *HttpServeMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.tracingHandler.ServeHTTP(w, r)
}

// StartSpan starts a new span named after the calling function.
func StartSpan(ctx context.Context, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	ctx, span := otel.GetTracerProvider().Tracer(buildBuddyInstrumentationName).Start(ctx, "unknown_go_function", opts...)
	if !span.IsRecording() {
		return ctx, span
	}

	rpc := make([]uintptr, 1)
	n := runtime.Callers(2, rpc[:])
	if n > 0 {
		frame, _ := runtime.CallersFrames(rpc).Next()
		span.SetName(filepath.Base(frame.Function))
	}
	return ctx, span
}

func AddStringAttributeToCurrentSpan(ctx context.Context, key, value string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.String(key, value))
}
