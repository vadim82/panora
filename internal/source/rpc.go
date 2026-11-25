package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const providerErrorCode = 1000

type failureData struct {
	Kind              string `json:"kind"`
	RetryAfterSeconds *int64 `json:"retry_after_seconds,omitempty"`
}

func validFailure(e *Error) bool {
	if !validErrorCode(e.Code) || strings.TrimSpace(e.Message) == "" || len(e.Message) > 4096 {
		return false
	}
	return e.RetryAfterSeconds == nil || (e.Code == "rate_limited" || e.Code == "unavailable") && *e.RetryAfterSeconds >= 0 && *e.RetryAfterSeconds <= 86400
}

func rpcFailure(err error) *RPCError {
	var typed *Error
	if errors.As(err, &typed) && validFailure(typed) {
		data, _ := json.Marshal(failureData{typed.Code, typed.RetryAfterSeconds})
		return &RPCError{Code: providerErrorCode, Message: typed.Message, Data: data}
	}
	return &RPCError{Code: -32603, Message: "Internal error"}
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(id))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case nil, string, json.Number:
		return true
	}
	return false
}

func invalidResponse(code int, message string) Response {
	return Response{JSONRPC: JSONRPC, ID: json.RawMessage("null"), Error: &RPCError{Code: code, Message: message}}
}

// Serve implements the bounded JSON-RPC 2.0 stdio profile: one JSON line and
// EOF per process, optionally containing up to MaxBatchSize calls. Notifications
// execute without replies. All replies share the same output byte budget.
func Serve(ctx context.Context, provider Provider, in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(in, MaxRequestBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxRequestBytes {
		return fmt.Errorf("plugin request exceeds %d bytes", MaxRequestBytes)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' || bytes.Contains(data[:len(data)-1], []byte{'\n'}) {
		return fmt.Errorf("expected one newline-terminated JSON request")
	}
	data = bytes.TrimSpace(data)
	var encoded []byte
	if !utf8.Valid(data) || !json.Valid(data) {
		encoded, err = json.Marshal(invalidResponse(-32700, "Parse error"))
	} else if data[0] == '[' {
		var requests []json.RawMessage
		_ = json.Unmarshal(data, &requests)
		if len(requests) == 0 || len(requests) > MaxBatchSize {
			encoded, err = json.Marshal(invalidResponse(-32600, "Invalid Request: batch must contain 1 to 100 calls"))
		} else {
			replies := make([]json.RawMessage, 0, len(requests))
			size := 2 // Brackets; each response also needs a comma or final newline.
			for _, request := range requests {
				if err := ctx.Err(); err != nil {
					return err
				}
				response := dispatch(ctx, provider, request)
				if response == nil {
					continue
				}
				reply, marshalErr := json.Marshal(response)
				if marshalErr != nil {
					return marshalErr
				}
				size += len(reply) + 1
				if size > MaxResponseBytes {
					return fmt.Errorf("plugin batch response exceeds %d bytes", MaxResponseBytes)
				}
				replies = append(replies, reply)
			}
			if len(replies) == 0 {
				return nil
			}
			encoded, err = json.Marshal(replies)
		}
	} else {
		response := dispatch(ctx, provider, data)
		if response == nil {
			return nil
		}
		encoded, err = json.Marshal(response)
	}
	if err != nil {
		return err
	}
	if len(encoded)+1 > MaxResponseBytes {
		return fmt.Errorf("plugin response exceeds %d bytes", MaxResponseBytes)
	}
	_, err = out.Write(append(encoded, '\n'))
	return err
}

func dispatch(ctx context.Context, provider Provider, data []byte) *Response {
	data = bytes.TrimSpace(data)
	invalid := invalidResponse(-32600, "Invalid Request")
	var request Request
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil || json.Unmarshal(fields["jsonrpc"], &request.JSONRPC) != nil || IsNull(fields["method"]) || json.Unmarshal(fields["method"], &request.Method) != nil || request.JSONRPC != JSONRPC {
		return &invalid
	}
	request.ID, request.Params = fields["id"], fields["params"]
	if len(request.ID) != 0 && !validID(request.ID) || len(request.Params) != 0 && (request.Params[0] != '{' && request.Params[0] != '[') {
		return &invalid
	}
	response := Response{JSONRPC: JSONRPC, ID: request.ID}
	result, rpcErr := invoke(ctx, provider, request.Method, request.Params)
	if len(request.ID) == 0 {
		return nil // A valid notification never receives a response, even on failure.
	}
	if rpcErr != nil {
		response.Error = rpcErr
	} else {
		var err error
		response.Result, err = json.Marshal(result)
		if err != nil {
			response.Error = rpcFailure(err)
		}
	}
	return &response
}

func invoke(ctx context.Context, provider Provider, method string, raw json.RawMessage) (any, *RPCError) {
	switch method {
	case "describe", "validate", "discover", "resolve":
	case SuggestMethod:
		if _, ok := provider.(Suggester); !ok {
			return nil, &RPCError{Code: -32601, Message: "Method not found"}
		}
	default:
		return nil, &RPCError{Code: -32601, Message: "Method not found"}
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(raw, &params) != nil || params == nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params: expected named parameters"}
	}
	var protocol int
	if json.Unmarshal(params["protocol"], &protocol) != nil || protocol != Protocol {
		return nil, rpcFailure(Failure("unsupported", "expected Panora provider protocol 2"))
	}
	delete(params, "protocol")
	allowed := map[string]bool{}
	switch method {
	case SuggestMethod:
		for _, name := range []string{"field", "prefix", "limit"} {
			allowed[name] = true
		}
	case "validate":
		allowed["config"] = true
	case "discover":
		for _, name := range []string{"config", "cursor", "limit", "filter"} {
			allowed[name] = true
		}
	case "resolve":
		for _, name := range []string{"config", "id", "ref"} {
			allowed[name] = true
		}
	}
	for name := range params {
		if !allowed[name] {
			return nil, &RPCError{Code: -32602, Message: "Invalid params"}
		}
	}
	data, _ := json.Marshal(params)
	invalidParams := &RPCError{Code: -32602, Message: "Invalid params"}
	var result any
	var err error
	switch method {
	case "describe":
		if Decode(data, &struct{}{}) != nil {
			return nil, invalidParams
		}
		result, err = provider.Describe(ctx)
	case "validate":
		var p struct {
			Config json.RawMessage `json:"config"`
		}
		if Decode(data, &p) != nil || len(p.Config) == 0 || IsNull(p.Config) {
			return nil, invalidParams
		}
		err = provider.Validate(ctx, p.Config)
		result = struct{}{}
	case "discover":
		var p DiscoverParams
		if Decode(data, &p) != nil || len(p.Config) == 0 || IsNull(p.Config) || len(p.Cursor) == 0 || len(p.Cursor) > MaxCursorBytes || p.Limit < 1 || p.Limit > MaxPageSize || p.Filter != nil && (p.Filter.MinWidth < 0 || p.Filter.MinHeight < 0 || !validDimensions(p.Filter.Ratio)) {
			return nil, invalidParams
		}
		result, err = provider.Discover(ctx, p)
	case SuggestMethod:
		var p SuggestParams
		if Decode(data, &p) != nil || !p.Valid() {
			return nil, invalidParams
		}
		values, suggestErr := provider.(Suggester).Suggest(ctx, p)
		response := suggestionResult{Values: values}
		if suggestErr == nil && !response.valid(p.Limit) {
			suggestErr = fmt.Errorf("provider returned invalid suggestions")
		}
		result, err = response, suggestErr
	case "resolve":
		var p ResolveParams
		if Decode(data, &p) != nil || len(p.Config) == 0 || IsNull(p.Config) || p.ID == "" || len(p.ID) > 4096 || len(p.Ref) == 0 || IsNull(p.Ref) {
			return nil, invalidParams
		}
		result, err = provider.Resolve(ctx, p)
	}
	if err != nil {
		return nil, rpcFailure(err)
	}
	return result, nil
}

// decodeResponse checks required members before unmarshalling: JSON null and
// missing fields must not turn into zero-valued successful Go structs.
func decodeResponse(line []byte, id string) (Response, error) {
	var response Response
	var fields map[string]json.RawMessage
	if !utf8.Valid(line) || json.Unmarshal(line, &fields) != nil || fields == nil {
		return response, fmt.Errorf("malformed JSON-RPC response")
	}
	response.ID = fields["id"]
	response.Result = fields["result"]
	if data, ok := fields["error"]; ok && !IsNull(data) {
		var e map[string]json.RawMessage
		if json.Unmarshal(data, &e) != nil || e == nil {
			return response, fmt.Errorf("invalid JSON-RPC error")
		}
		var rpc RPCError
		if len(e["code"]) == 0 || IsNull(e["code"]) || json.Unmarshal(e["code"], &rpc.Code) != nil || json.Unmarshal(e["message"], &rpc.Message) != nil || rpc.Message == "" || len(rpc.Message) > 4096 {
			return response, fmt.Errorf("invalid JSON-RPC error")
		}
		rpc.Data = e["data"]
		response.Error = &rpc
	}
	var actualID string
	var version string
	_, hasResult := fields["result"]
	_, hasError := fields["error"]
	if json.Unmarshal(fields["jsonrpc"], &version) != nil || version != JSONRPC || json.Unmarshal(fields["id"], &actualID) != nil || actualID != id || hasResult == hasError || hasResult && IsNull(response.Result) || hasError && response.Error == nil {
		return response, fmt.Errorf("invalid JSON-RPC response envelope")
	}
	response.JSONRPC = version
	if hasError {
		if response.Error.Code == providerErrorCode {
			var data failureData
			if Decode(response.Error.Data, &data) != nil || !validFailure(&Error{Code: data.Kind, Message: response.Error.Message, RetryAfterSeconds: data.RetryAfterSeconds}) {
				return response, fmt.Errorf("invalid provider failure")
			}
			return response, &Error{Code: data.Kind, Message: response.Error.Message, RetryAfterSeconds: data.RetryAfterSeconds}
		}
		return response, response.Error
	}
	return response, nil
}
