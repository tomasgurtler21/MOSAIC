package descriptor

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
	"mosaic-deploy/internal/domain"
)

// wireDescriptor is the YAML wire representation of a harness descriptor.
// It carries struct tags; domain.HarnessDescriptor does not (CD-1).
type wireDescriptor struct {
	SchemaVersion     string              `yaml:"schema_version"`
	ID                string              `yaml:"id"`
	DisplayName       string              `yaml:"display_name"`
	TransformVersion  string              `yaml:"transform_version"`
	InjectionsVersion string              `yaml:"injections_version"`
	Models            wireModelCatalog    `yaml:"models"`
	Tools             wireToolSpec        `yaml:"tools"`
	Paths             wirePathSpec        `yaml:"paths"`
	Extensions        map[string]string   `yaml:"extensions"`
	Frontmatter       wireFrontmatterSpec `yaml:"frontmatter"`
	Injections        []wireInjection     `yaml:"injections"`
	Hooks             wireHookSupport     `yaml:"hooks"`
}

type wireModelCatalog struct {
	IDs        []string `yaml:"ids"`
	FormatHint string   `yaml:"format_hint"`
}

type wireToolSpec struct {
	Shape                  string                      `yaml:"shape"`
	Universe               []wireHarnessTool           `yaml:"universe"`
	Mappings               []wireToolMapping           `yaml:"mappings"`
	CustomToolTemplate     string                      `yaml:"custom_tool_template"`
	CustomToolDestinations []wireCustomToolDestination `yaml:"custom_tool_destination"`
	PlaceholderExpansion   []string                    `yaml:"placeholder_expansion"`
}

// wireCustomToolDestination is the wire form of one harness-level default custom-tool
// destination. It deliberately omits a Names member: names identify already-known tool
// names, which is meaningless for a default applying to every not-yet-resolved custom
// tool. Because Parse decodes with yaml.DisallowUnknownField(), the absence of the
// member is itself the rejection mechanism for a "names:" key.
type wireCustomToolDestination struct {
	To        string `yaml:"to"`
	Field     string `yaml:"field"`
	Format    string `yaml:"format"`
	Separator string `yaml:"separator"`
}

type wireHarnessTool struct {
	Name         string `yaml:"name"`
	Unused       string `yaml:"unused"`
	ByConvention bool   `yaml:"by_convention"`
}

type wireToolMapping struct {
	Generic      string                `yaml:"generic"`
	Destinations []wireToolDestination `yaml:"destinations"`
}

type wireToolDestination struct {
	To        string   `yaml:"to"`
	Field     string   `yaml:"field"`
	Format    string   `yaml:"format"`
	Separator string   `yaml:"separator"`
	Names     []string `yaml:"names"`
}

type wirePathSpec struct {
	Agents wireScopedPaths `yaml:"agents"`
	Skills wireScopedPaths `yaml:"skills"`
	Hooks  wireScopedPaths `yaml:"hooks"`
}

type wireScopedPaths struct {
	Supported bool   `yaml:"supported"`
	Project   string `yaml:"project"`
}

type wireFrontmatterSpec struct {
	ModelKey string                 `yaml:"model_key"`
	ToolsKey string                 `yaml:"tools_key"`
	Add      []wireFrontmatterField `yaml:"add"`
	Drop     []string               `yaml:"drop"`
	KeyOrder []string               `yaml:"key_order"`
}

// wireFrontmatterField holds an add-field entry: a key and an arbitrary YAML value.
type wireFrontmatterField struct {
	Key   string      `yaml:"key"`
	Value interface{} `yaml:"value"`
}

type wireInjection struct {
	Name    string `yaml:"name"`
	Content string `yaml:"content"`
}

type wireHookSupport struct {
	Supported  bool   `yaml:"supported"`
	VariantKey string `yaml:"variant_key"`
}

// Load reads and validates a harness descriptor from a YAML file at path.
// It returns a validated HarnessDescriptor on success, or an error describing what was wrong
// (including file name, field, and reason). ErrUnsupportedSchemaVersion is returned when the
// descriptor's schema_version is not in SupportedSchemaVersions.
func Load(path string) (*domain.HarnessDescriptor, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read descriptor %s: %w", path, err)
	}
	return Parse(src, path)
}

// Parse validates descriptor bytes and maps them to a HarnessDescriptor. origin is used as the
// file name in any ValidationError values returned (useful for embedded built-in descriptors
// that have no disk path).
// It returns ErrUnsupportedSchemaVersion when the descriptor's schema_version is not supported.
func Parse(src []byte, origin string) (*domain.HarnessDescriptor, error) {
	var w wireDescriptor
	if err := yaml.UnmarshalWithOptions(src, &w, yaml.DisallowUnknownField(), yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}

	// Check schema version before other validation. A declared-but-unsupported version is a
	// distinct error from a missing version (which Validate catches as a required-field error).
	if w.SchemaVersion != "" {
		supported := false
		for _, v := range SupportedSchemaVersions {
			if w.SchemaVersion == v {
				supported = true
				break
			}
		}
		if !supported {
			return nil, fmt.Errorf("%s: schema_version %q: %w", origin, w.SchemaVersion, ErrUnsupportedSchemaVersion)
		}
	}

	d := mapWireToDomain(&w)

	errs := Validate(d)
	if len(errs) > 0 {
		// Enrich each error with the origin file path, then return the first.
		// When there are multiple problems, the first error's message notes the total
		// count so descriptor authors know there is more than one issue to fix.
		for i := range errs {
			errs[i].File = origin
		}
		first := errs[0]
		if len(errs) > 1 {
			first.Message = fmt.Sprintf("%s (and %d more validation error(s))", first.Message, len(errs)-1)
		}
		return nil, first
	}

	return d, nil
}

// mapWireToDomain converts the YAML wire representation to the domain type.
func mapWireToDomain(w *wireDescriptor) *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion:     w.SchemaVersion,
		ID:                w.ID,
		DisplayName:       w.DisplayName,
		TransformVersion:  w.TransformVersion,
		InjectionsVersion: w.InjectionsVersion,
		Models:            mapWireModelCatalog(&w.Models),
		Tools:             mapWireToolSpec(&w.Tools),
		Paths:             mapWirePathSpec(&w.Paths),
		Extensions:        mapExtensions(w.Extensions),
		Frontmatter:       mapWireFrontmatterSpec(&w.Frontmatter),
		Injections:        mapWireInjections(w.Injections),
		Hooks: domain.HookSupport{
			Supported:  w.Hooks.Supported,
			VariantKey: w.Hooks.VariantKey,
		},
	}
}

func mapWireModelCatalog(w *wireModelCatalog) domain.ModelCatalog {
	return domain.ModelCatalog{
		IDs:        w.IDs,
		FormatHint: w.FormatHint,
	}
}

func mapWireToolSpec(w *wireToolSpec) domain.ToolSpec {
	universe := make([]domain.HarnessTool, len(w.Universe))
	for i, t := range w.Universe {
		universe[i] = domain.HarnessTool{
			Name:         t.Name,
			Unused:       domain.Disposition(t.Unused),
			ByConvention: t.ByConvention,
		}
	}

	mappings := make([]domain.ToolMapping, len(w.Mappings))
	for i, m := range w.Mappings {
		// make always produces a non-nil slice, so destinations: [] decodes as empty (non-nil).
		dests := make([]domain.ToolDestination, len(m.Destinations))
		for j, d := range m.Destinations {
			names := make([]string, len(d.Names))
			copy(names, d.Names)
			dests[j] = domain.ToolDestination{
				Kind:      domain.ToolDestinationKind(d.To),
				Field:     d.Field,
				Format:    domain.ToolValueFormat(d.Format),
				Separator: d.Separator,
				Names:     names,
			}
		}
		mappings[i] = domain.ToolMapping{
			Generic:      m.Generic,
			Destinations: dests,
		}
	}

	customDests := make([]domain.ToolDestination, len(w.CustomToolDestinations))
	for i, d := range w.CustomToolDestinations {
		customDests[i] = domain.ToolDestination{
			Kind:      domain.ToolDestinationKind(d.To),
			Field:     d.Field,
			Format:    domain.ToolValueFormat(d.Format),
			Separator: d.Separator,
			Names:     []string{},
		}
	}

	return domain.ToolSpec{
		Shape:                  domain.ToolShape(w.Shape),
		Universe:               universe,
		Mappings:               mappings,
		CustomToolTemplate:     w.CustomToolTemplate,
		CustomToolDestinations: customDests,
		PlaceholderExpansion:   w.PlaceholderExpansion,
	}
}

func mapWirePathSpec(w *wirePathSpec) domain.PathSpec {
	return domain.PathSpec{
		Agents: mapWireScopedPaths(&w.Agents),
		Skills: mapWireScopedPaths(&w.Skills),
		Hooks:  mapWireScopedPaths(&w.Hooks),
	}
}

func mapWireScopedPaths(w *wireScopedPaths) domain.ScopedPaths {
	return domain.ScopedPaths{
		Supported: w.Supported,
		Project:   w.Project,
	}
}

func mapExtensions(m map[string]string) map[domain.ArtifactKind]string {
	if m == nil {
		return nil
	}
	result := make(map[domain.ArtifactKind]string, len(m))
	for k, v := range m {
		result[domain.ArtifactKind(k)] = v
	}
	return result
}

func mapWireFrontmatterSpec(w *wireFrontmatterSpec) domain.FrontmatterSpec {
	add := make([]domain.FrontmatterField, len(w.Add))
	for i, f := range w.Add {
		add[i] = domain.FrontmatterField{
			Key:   f.Key,
			Value: mapWireFieldValue(f.Value),
		}
	}
	return domain.FrontmatterSpec{
		ModelKey: w.ModelKey,
		ToolsKey: w.ToolsKey,
		Add:      add,
		Drop:     w.Drop,
		KeyOrder: w.KeyOrder,
	}
}

// mapWireFieldValue converts an arbitrary YAML value (unmarshalled into interface{}) to
// a domain.FieldValue. Scalars become KindScalar, sequences become KindList, and mappings
// become KindMapping.
//
// The caller must decode with yaml.UseOrderedMap() so that YAML mappings arrive as
// yaml.MapSlice ([]yaml.MapItem) rather than map[string]interface{}. This guarantees
// that domain.FieldValue.Pairs preserves YAML source key order, which is required by the
// "order-significant" contract on that field.
func mapWireFieldValue(v interface{}) domain.FieldValue {
	if v == nil {
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: ""}
	}
	switch val := v.(type) {
	case string:
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: val}
	case bool:
		if val {
			return domain.FieldValue{Kind: domain.KindScalar, Scalar: "true"}
		}
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: "false"}
	case int:
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: fmt.Sprintf("%d", val)}
	case int64:
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: fmt.Sprintf("%d", val)}
	case float64:
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: fmt.Sprintf("%g", val)}
	case []interface{}:
		items := make([]domain.FieldValue, len(val))
		for i, item := range val {
			items[i] = mapWireFieldValue(item)
		}
		return domain.FieldValue{Kind: domain.KindList, Items: items}
	case yaml.MapSlice:
		// yaml.UseOrderedMap() causes YAML mapping nodes decoded as interface{} to arrive
		// here as yaml.MapSlice, which preserves the source key order.
		pairs := make([]domain.FieldPair, len(val))
		for i, item := range val {
			pairs[i] = domain.FieldPair{
				Key:   fmt.Sprint(item.Key),
				Value: mapWireFieldValue(item.Value),
			}
		}
		return domain.FieldValue{Kind: domain.KindMapping, Pairs: pairs}
	default:
		return domain.FieldValue{Kind: domain.KindScalar, Scalar: fmt.Sprint(v)}
	}
}

func mapWireInjections(ws []wireInjection) []domain.InjectionContent {
	if len(ws) == 0 {
		return nil
	}
	result := make([]domain.InjectionContent, len(ws))
	for i, w := range ws {
		result[i] = domain.InjectionContent{
			Name:    w.Name,
			Content: w.Content,
		}
	}
	return result
}
