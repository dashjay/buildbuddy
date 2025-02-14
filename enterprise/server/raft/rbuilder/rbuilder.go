package rbuilder

import (
	"fmt"

	"github.com/buildbuddy-io/buildbuddy/server/util/status"
	"github.com/golang/protobuf/proto"

	rfpb "github.com/buildbuddy-io/buildbuddy/proto/raft"
	gstatus "google.golang.org/grpc/status"
)

type BatchBuilder struct {
	cmd *rfpb.BatchCmdRequest
	err error
}

func NewBatchBuilder() *BatchBuilder {
	return &BatchBuilder{
		cmd: &rfpb.BatchCmdRequest{},
		err: nil,
	}
}

func (bb *BatchBuilder) setErr(err error) {
	if bb.err != nil {
		return
	}
	bb.err = err
}

func (bb *BatchBuilder) Merge(bb2 *BatchBuilder) *BatchBuilder {
	proto.Merge(bb.cmd, bb2.cmd)
	return bb
}

func (bb *BatchBuilder) Add(m proto.Message) *BatchBuilder {
	if bb.cmd == nil {
		bb.cmd = &rfpb.BatchCmdRequest{}
	}

	req := &rfpb.RequestUnion{}
	switch value := m.(type) {
	case *rfpb.FileWriteRequest:
		req.Value = &rfpb.RequestUnion_FileWrite{
			FileWrite: value,
		}
	case *rfpb.DirectReadRequest:
		req.Value = &rfpb.RequestUnion_DirectRead{
			DirectRead: value,
		}
	case *rfpb.DirectWriteRequest:
		req.Value = &rfpb.RequestUnion_DirectWrite{
			DirectWrite: value,
		}
	case *rfpb.IncrementRequest:
		req.Value = &rfpb.RequestUnion_Increment{
			Increment: value,
		}
	case *rfpb.ScanRequest:
		req.Value = &rfpb.RequestUnion_Scan{
			Scan: value,
		}
	case *rfpb.CASRequest:
		req.Value = &rfpb.RequestUnion_Cas{
			Cas: value,
		}
	default:
		bb.setErr(status.FailedPreconditionErrorf("BatchBuilder.Add handling for %+v not implemented.", m))
		return bb
	}

	bb.cmd.Union = append(bb.cmd.Union, req)
	return bb
}

func (bb *BatchBuilder) ToProto() (*rfpb.BatchCmdRequest, error) {
	if bb.err != nil {
		return nil, bb.err
	}
	return bb.cmd, nil
}

func (bb *BatchBuilder) ToBuf() ([]byte, error) {
	if bb.err != nil {
		return nil, bb.err
	}
	return proto.Marshal(bb.cmd)
}

func (bb *BatchBuilder) String() string {
	builder := fmt.Sprintf("Builder(err: %s)", bb.err)
	for i, v := range bb.cmd.Union {
		builder += fmt.Sprintf(" [%d]: %+v", i, proto.CompactTextString(v))
	}
	return builder
}

type BatchResponse struct {
	cmd *rfpb.BatchCmdResponse
	err error
}

func (br *BatchResponse) setErr(err error) {
	if br.err != nil {
		return
	}
	br.err = err
}

func NewBatchResponse(val interface{}) *BatchResponse {
	br := &BatchResponse{
		cmd: &rfpb.BatchCmdResponse{},
	}

	buf, ok := val.([]byte)
	if !ok {
		br.setErr(status.FailedPreconditionError("Could not coerce value to []byte."))
	}
	if err := proto.Unmarshal(buf, br.cmd); err != nil {
		br.setErr(err)
	}
	return br
}

func NewBatchResponseFromProto(c *rfpb.BatchCmdResponse) *BatchResponse {
	return &BatchResponse{
		cmd: c,
	}
}

func (br *BatchResponse) checkIndex(n int) {
	if n >= len(br.cmd.GetUnion()) {
		br.setErr(status.FailedPreconditionErrorf("batch did not contain %d elements", n))
	}
}

func (br *BatchResponse) unionError(u *rfpb.ResponseUnion) error {
	s := gstatus.FromProto(u.GetStatus())
	return s.Err()
}

func (br *BatchResponse) DirectReadResponse(n int) (*rfpb.DirectReadResponse, error) {
	br.checkIndex(n)
	if br.err != nil {
		return nil, br.err
	}
	u := br.cmd.GetUnion()[n]
	return u.GetDirectRead(), br.unionError(u)
}

func (br *BatchResponse) IncrementResponse(n int) (*rfpb.IncrementResponse, error) {
	br.checkIndex(n)
	if br.err != nil {
		return nil, br.err
	}
	u := br.cmd.GetUnion()[n]
	return u.GetIncrement(), br.unionError(u)
}

func (br *BatchResponse) ScanResponse(n int) (*rfpb.ScanResponse, error) {
	br.checkIndex(n)
	if br.err != nil {
		return nil, br.err
	}
	u := br.cmd.GetUnion()[n]
	return u.GetScan(), br.unionError(u)
}

func (br *BatchResponse) CASResponse(n int) (*rfpb.CASResponse, error) {
	br.checkIndex(n)
	if br.err != nil {
		return nil, br.err
	}
	u := br.cmd.GetUnion()[n]
	return u.GetCas(), br.unionError(u)
}
