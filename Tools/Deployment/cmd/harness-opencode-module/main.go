// Command harness-opencode-module is the external harness module process for the OpenCode
// harness. It implements the JSON-over-stdio protocol defined in internal/harness/external,
// reading single-line JSON requests from stdin and writing single-line JSON responses to stdout.
//
// The process lifecycle is:
//  1. Receive a "handshake" request on stdin; respond with protocol version and harness identity.
//  2. Receive method requests in a loop; dispatch each to the in-process OpenCode module.
//  3. Terminate when stdin is closed (host process went away).
//
// Source sharing: this binary imports opencode.New() from the same package as the built-in
// harness, satisfying AC23.2 (reference module built from shared source).
//
// Protocol: one JSON object per line, UTF-8, no BOM. Requests include: protocol, id, method,
// params. Responses include: protocol, id, and one of result or error. The protocol field on
// every response must equal the ProtocolVersion constant "1.0".
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/opencode"
)

// protocolVersion is the version this server speaks. It must equal external.ProtocolVersion.
const protocolVersion = "1.0"

// ---------------------------------------------------------------------------
// Wire types (server-side, mirroring the adapter's unexported types)
// ---------------------------------------------------------------------------

type wireRequest struct {
	Protocol string          `json:"protocol"`
	ID       string          `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type wireResponse struct {
	Protocol string          `json:"protocol"`
	ID       string          `json:"id"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *wireRespError  `json:"error,omitempty"`
}

type wireRespError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wireFieldValue struct {
	Kind    string           `json:"kind"`
	Scalar  string           `json:"scalar,omitempty"`
	Quote   string           `json:"quote,omitempty"`
	Items   []wireFieldValue `json:"items,omitempty"`
	List    string           `json:"list,omitempty"`
	Pairs   []wireFieldPair  `json:"pairs,omitempty"`
	Comment string           `json:"comment,omitempty"`
}

type wireFieldPair struct {
	Key   string         `json:"key"`
	Value wireFieldValue `json:"value"`
}

type wireFrontmatterField struct {
	Key   string         `json:"key"`
	Value wireFieldValue `json:"value"`
}

// ---------------------------------------------------------------------------
// Param types (received from adapter)
// ---------------------------------------------------------------------------

type wireToolRequest struct {
	AgentKey     string            `json:"agent_key"`
	Generic      []string          `json:"generic,omitempty"`
	Placeholder  string            `json:"placeholder,omitempty"`
	CustomNames  map[string]string `json:"custom_names,omitempty"`
	SkippedTools map[string]bool   `json:"skipped_tools,omitempty"`
}

type wireFrontmatterRequest struct {
	Kind       string                 `json:"kind"`
	AgentKey   string                 `json:"agent_key"`
	Source     []wireFrontmatterField `json:"source,omitempty"`
	Model      wireModelSelection     `json:"model"`
	ToolFields []wireFrontmatterField `json:"tool_fields,omitempty"`
	Versions   wireVersionStamps      `json:"versions"`
}

type wireModelSelection struct {
	ModelID string `json:"model_id"`
	Origin  string `json:"origin"`
}

type wireVersionStamps struct {
	Version                       string `json:"version"`
	TransformVersion              string `json:"transform_version"`
	InjectionsVersion             string `json:"injections_version"`
	OrchestratorInjectionsVersion string `json:"orchestrator_injections_version,omitempty"`
}

type wireTargetPathRequest struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	FileName string `json:"file_name,omitempty"`
	Scope    string `json:"scope"`
	GOOS     string `json:"goos"`
}

type wireInjectionRequest struct {
	Name     string `json:"name"`
	AgentKey string `json:"agent_key,omitempty"`
}

type wireHookPlanRequest struct {
	Bundle wireHookBundle `json:"bundle"`
	Scope  string         `json:"scope"`
}

type wireHookBundle struct {
	Key         string                     `json:"key"`
	Version     string                     `json:"version,omitempty"`
	Description string                     `json:"description,omitempty"`
	SourceDir   string                     `json:"source_dir,omitempty"`
	Variants    map[string]wireHookVariant `json:"variants,omitempty"`
	Placeholder bool                       `json:"placeholder,omitempty"`
}

type wireHookVariant struct {
	HarnessID     string          `json:"harness_id"`
	Supported     bool            `json:"supported"`
	ReusesVariant string          `json:"reuses_variant,omitempty"`
	Files         []wireHookFile  `json:"files,omitempty"`
	Registration  []wireRegStep   `json:"registration,omitempty"`
}

type wireHookFile struct {
	SourcePath string `json:"source_path"`
	TargetName string `json:"target_name"`
}

type wireRegStep struct {
	ID          string `json:"id"`
	TargetPath  string `json:"target_path,omitempty"`
	Fragment    string `json:"fragment,omitempty"`
	Performable bool   `json:"performable,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

// ---------------------------------------------------------------------------
// Result types (sent to adapter)
// ---------------------------------------------------------------------------

type wireToolResult struct {
	Fields      []wireFrontmatterField `json:"fields"`
	Resolutions []wireToolResolution   `json:"resolutions"`
}

type wireToolResolution struct {
	Generic      string   `json:"generic"`
	Outcome      string   `json:"outcome"`
	HarnessTools []string `json:"harness_tools,omitempty"`
	Field        string   `json:"field,omitempty"`
}

type wireFrontmatterPlan struct {
	Set      []wireFrontmatterField `json:"set,omitempty"`
	Remove   []string               `json:"remove,omitempty"`
	KeyOrder []string               `json:"key_order,omitempty"`
}

type wireTargetPathResult struct {
	Path string `json:"path"`
}

type wireInjectionResult struct {
	Content string `json:"content"`
	OK      bool   `json:"ok"`
}

type wireHookPlanResult struct {
	Supported    bool           `json:"supported"`
	Reason       string         `json:"reason,omitempty"`
	Files        []wireHookFile `json:"files,omitempty"`
	TargetDir    string         `json:"target_dir,omitempty"`
	Registration []wireRegStep  `json:"registration,omitempty"`
}

// ---------------------------------------------------------------------------
// Domain ↔ wire conversions (server side)
// ---------------------------------------------------------------------------

func toWireFieldValue(fv domain.FieldValue) wireFieldValue {
	w := wireFieldValue{
		Kind:    string(fv.Kind),
		Scalar:  fv.Scalar,
		Quote:   string(fv.Quote),
		List:    string(fv.List),
		Comment: fv.Comment,
	}
	if fv.Items != nil {
		w.Items = make([]wireFieldValue, len(fv.Items))
		for i, item := range fv.Items {
			w.Items[i] = toWireFieldValue(item)
		}
	}
	if fv.Pairs != nil {
		w.Pairs = make([]wireFieldPair, len(fv.Pairs))
		for i, pair := range fv.Pairs {
			w.Pairs[i] = wireFieldPair{Key: pair.Key, Value: toWireFieldValue(pair.Value)}
		}
	}
	return w
}

func toWireFrontmatterField(f domain.FrontmatterField) wireFrontmatterField {
	return wireFrontmatterField{Key: f.Key, Value: toWireFieldValue(f.Value)}
}

func toWireFrontmatterFields(fs []domain.FrontmatterField) []wireFrontmatterField {
	if fs == nil {
		return nil
	}
	result := make([]wireFrontmatterField, len(fs))
	for i, f := range fs {
		result[i] = toWireFrontmatterField(f)
	}
	return result
}

func fromWireFrontmatterField(w wireFrontmatterField) domain.FrontmatterField {
	return domain.FrontmatterField{Key: w.Key, Value: fromWireFieldValue(w.Value)}
}

func fromWireFieldValue(w wireFieldValue) domain.FieldValue {
	fv := domain.FieldValue{
		Kind:    domain.ValueKind(w.Kind),
		Scalar:  w.Scalar,
		Quote:   domain.QuoteStyle(w.Quote),
		List:    domain.ListStyle(w.List),
		Comment: w.Comment,
	}
	if w.Items != nil {
		fv.Items = make([]domain.FieldValue, len(w.Items))
		for i, item := range w.Items {
			fv.Items[i] = fromWireFieldValue(item)
		}
	}
	if w.Pairs != nil {
		fv.Pairs = make([]domain.FieldPair, len(w.Pairs))
		for i, pair := range w.Pairs {
			fv.Pairs[i] = domain.FieldPair{Key: pair.Key, Value: fromWireFieldValue(pair.Value)}
		}
	}
	return fv
}

func fromWireFrontmatterFields(ws []wireFrontmatterField) []domain.FrontmatterField {
	if ws == nil {
		return nil
	}
	fs := make([]domain.FrontmatterField, len(ws))
	for i, w := range ws {
		fs[i] = fromWireFrontmatterField(w)
	}
	return fs
}

func fromWireHookBundle(wb wireHookBundle) domain.HookBundle {
	b := domain.HookBundle{
		Key:         wb.Key,
		Version:     wb.Version,
		Description: wb.Description,
		SourceDir:   wb.SourceDir,
		Placeholder: wb.Placeholder,
	}
	if wb.Variants != nil {
		b.Variants = make(map[string]domain.HookVariant, len(wb.Variants))
		for k, v := range wb.Variants {
			hv := domain.HookVariant{
				HarnessID:     v.HarnessID,
				Supported:     v.Supported,
				ReusesVariant: v.ReusesVariant,
			}
			if v.Files != nil {
				hv.Files = make([]domain.HookFile, len(v.Files))
				for i, f := range v.Files {
					hv.Files[i] = domain.HookFile{SourcePath: f.SourcePath, TargetName: f.TargetName}
				}
			}
			if v.Registration != nil {
				hv.Registration = make([]domain.RegistrationStep, len(v.Registration))
				for i, s := range v.Registration {
					hv.Registration[i] = domain.RegistrationStep{
						ID: s.ID, TargetPath: s.TargetPath, Fragment: s.Fragment,
						Performable: s.Performable, Instruction: s.Instruction,
					}
				}
			}
			b.Variants[k] = hv
		}
	}
	return b
}

func toWireHookPlanResult(plan domain.HookPlan) wireHookPlanResult {
	result := wireHookPlanResult{
		Supported: plan.Supported,
		Reason:    plan.Reason,
		TargetDir: plan.TargetDir,
	}
	if plan.Files != nil {
		result.Files = make([]wireHookFile, len(plan.Files))
		for i, f := range plan.Files {
			result.Files[i] = wireHookFile{SourcePath: f.SourcePath, TargetName: f.TargetName}
		}
	}
	if plan.Registration != nil {
		result.Registration = make([]wireRegStep, len(plan.Registration))
		for i, s := range plan.Registration {
			result.Registration[i] = wireRegStep{
				ID: s.ID, TargetPath: s.TargetPath, Fragment: s.Fragment,
				Performable: s.Performable, Instruction: s.Instruction,
			}
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Server loop
// ---------------------------------------------------------------------------

func main() {
	mod, err := opencode.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness-opencode-module: init: %v\n", err)
		os.Exit(1)
	}

	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req wireRequest
		if err := dec.Decode(&req); err != nil {
			// stdin closed: host process went away, terminate cleanly.
			os.Exit(0)
		}

		resp := handleRequest(mod, req)
		if err := enc.Encode(resp); err != nil {
			fmt.Fprintf(os.Stderr, "harness-opencode-module: write response: %v\n", err)
			os.Exit(1)
		}
	}
}

func handleRequest(mod domain.HarnessModule, req wireRequest) wireResponse {
	base := wireResponse{Protocol: protocolVersion, ID: req.ID}

	switch req.Method {
	case "handshake":
		ref := mod.Ref()
		result := map[string]any{
			"protocol": protocolVersion,
			"harness": map[string]any{
				"id":           ref.ID,
				"display_name": ref.DisplayName,
				"tier":         string(ref.Tier),
				"usable":       ref.Usable,
			},
		}
		raw, _ := json.Marshal(result)
		base.Result = raw

	case "tools":
		var params wireToolRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			base.Error = &wireRespError{Code: "bad_params", Message: err.Error()}
			return base
		}
		domReq := domain.ToolRequest{
			AgentKey:     params.AgentKey,
			Generic:      params.Generic,
			Placeholder:  params.Placeholder,
			CustomNames:  params.CustomNames,
			SkippedTools: params.SkippedTools,
		}
		toolResult, err := mod.Tools(domReq)
		if err != nil {
			base.Error = &wireRespError{Code: "internal", Message: err.Error()}
			return base
		}
		resolutions := make([]wireToolResolution, len(toolResult.Resolutions))
		for i, r := range toolResult.Resolutions {
			resolutions[i] = wireToolResolution{
				Generic:      r.Generic,
				Outcome:      string(r.Outcome),
				HarnessTools: r.HarnessTools,
				Field:        r.Field,
			}
		}
		result := wireToolResult{
			Fields:      toWireFrontmatterFields(toolResult.Fields),
			Resolutions: resolutions,
		}
		// Ensure nil slices become [] in JSON for round-trip correctness.
		if result.Fields == nil {
			result.Fields = []wireFrontmatterField{}
		}
		if result.Resolutions == nil {
			result.Resolutions = []wireToolResolution{}
		}
		raw, _ := json.Marshal(result)
		base.Result = raw

	case "frontmatter":
		var params wireFrontmatterRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			base.Error = &wireRespError{Code: "bad_params", Message: err.Error()}
			return base
		}
		domReq := domain.FrontmatterRequest{
			Kind:       domain.ArtifactKind(params.Kind),
			AgentKey:   params.AgentKey,
			Source:     fromWireFrontmatterFields(params.Source),
			Model:      domain.ModelSelection{ModelID: params.Model.ModelID, Origin: domain.ModelOrigin(params.Model.Origin)},
			ToolFields: fromWireFrontmatterFields(params.ToolFields),
			Versions: domain.VersionStamps{
				Version:                       params.Versions.Version,
				TransformVersion:              params.Versions.TransformVersion,
				InjectionsVersion:             params.Versions.InjectionsVersion,
				OrchestratorInjectionsVersion: params.Versions.OrchestratorInjectionsVersion,
			},
		}
		plan, err := mod.Frontmatter(domReq)
		if err != nil {
			base.Error = &wireRespError{Code: "internal", Message: err.Error()}
			return base
		}
		result := wireFrontmatterPlan{
			Set:      toWireFrontmatterFields(plan.Set),
			Remove:   plan.Remove,
			KeyOrder: plan.KeyOrder,
		}
		raw, _ := json.Marshal(result)
		base.Result = raw

	case "target_path":
		var params wireTargetPathRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			base.Error = &wireRespError{Code: "bad_params", Message: err.Error()}
			return base
		}
		domReq := domain.TargetPathRequest{
			Kind:     domain.ArtifactKind(params.Kind),
			Key:      params.Key,
			FileName: params.FileName,
			Scope:    domain.Scope(params.Scope),
			GOOS:     params.GOOS,
		}
		p, err := mod.TargetPath(domReq)
		if err != nil {
			code := "internal"
			if isArtifactUnsupported(err) {
				code = "unsupported_artifact"
			} else if isScopeUnsupported(err) {
				code = "unsupported_scope"
			}
			base.Error = &wireRespError{Code: code, Message: err.Error()}
			return base
		}
		result := wireTargetPathResult{Path: p}
		raw, _ := json.Marshal(result)
		base.Result = raw

	case "injection":
		var params wireInjectionRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			base.Error = &wireRespError{Code: "bad_params", Message: err.Error()}
			return base
		}
		content, ok := mod.Injection(domain.InjectionRequest{Name: params.Name, AgentKey: params.AgentKey})
		result := wireInjectionResult{Content: content, OK: ok}
		raw, _ := json.Marshal(result)
		base.Result = raw

	case "hook_plan":
		var params wireHookPlanRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			base.Error = &wireRespError{Code: "bad_params", Message: err.Error()}
			return base
		}
		domReq := domain.HookPlanRequest{
			Bundle: fromWireHookBundle(params.Bundle),
			Scope:  domain.Scope(params.Scope),
		}
		plan, err := mod.HookPlan(domReq)
		if err != nil {
			base.Error = &wireRespError{Code: "internal", Message: err.Error()}
			return base
		}
		result := toWireHookPlanResult(plan)
		raw, _ := json.Marshal(result)
		base.Result = raw

	default:
		base.Error = &wireRespError{
			Code:    "unsupported_method",
			Message: fmt.Sprintf("unknown method: %s", req.Method),
		}
	}

	return base
}

// isArtifactUnsupported returns true if err wraps domain.ErrArtifactUnsupported.
func isArtifactUnsupported(err error) bool {
	if err == nil {
		return false
	}
	// Use string unwrapping since errors.Is works via Unwrap chain.
	// The domain sentinel uses fmt.Errorf("%w: ...", domain.ErrArtifactUnsupported) so we
	// can use the standard errors package to check it.
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if err == domain.ErrArtifactUnsupported {
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}

// isScopeUnsupported returns true if err wraps domain.ErrUnsupportedScope.
func isScopeUnsupported(err error) bool {
	if err == nil {
		return false
	}
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if err == domain.ErrUnsupportedScope {
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}
