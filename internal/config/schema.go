package config

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// configSchemaJSON is the machine-readable schema for config.yaml. It is
// embedded so the binary validates against the schema that shipped with it.
//
//go:embed config.schema.json
var configSchemaJSON []byte

// configSchemaURL must match the $id in config.schema.json. It gives the
// embedded schema a stable canonical URL for internal $ref resolution.
const configSchemaURL = "https://raw.githubusercontent.com/y0n1/virtlab/main/internal/config/config.schema.json"

var (
	configSchemaOnce sync.Once
	configSchema     *jsonschema.Schema
	configSchemaErr  error
)

// ValidateSchema checks YAML config data against the embedded JSON schema. It
// reports unknown keys, wrong types, and values outside the documented format.
// Domain rules that depend on defaults or on the Host stay in ValidateStatic
// and the other validation functions.
func ValidateSchema(data []byte) error {
	schema, err := compiledConfigSchema()
	if err != nil {
		return err
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse yaml for schema validation: %w", err)
	}

	normalized, err := json.Marshal(toJSONValue(doc))
	if err != nil {
		return fmt.Errorf("convert yaml for schema validation: %w", err)
	}
	var instance any
	if err := json.Unmarshal(normalized, &instance); err != nil {
		return fmt.Errorf("convert yaml for schema validation: %w", err)
	}

	if err := schema.Validate(instance); err != nil {
		return formatSchemaError(err)
	}
	return nil
}

func compiledConfigSchema() (*jsonschema.Schema, error) {
	configSchemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(configSchemaURL, bytes.NewReader(configSchemaJSON)); err != nil {
			configSchemaErr = fmt.Errorf("load embedded schema: %w", err)
			return
		}
		configSchema, configSchemaErr = compiler.Compile(configSchemaURL)
		if configSchemaErr != nil {
			configSchemaErr = fmt.Errorf("compile embedded schema: %w", configSchemaErr)
		}
	})
	return configSchema, configSchemaErr
}

// toJSONValue converts YAML-decoded values into types that encoding/json
// accepts. yaml.v3 normally yields map[string]any, but older documents and
// non-string keys still decode to map[any]any.
func toJSONValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, item := range value {
			value[key] = toJSONValue(item)
		}
		return value
	case map[any]any:
		converted := make(map[string]any, len(value))
		for key, item := range value {
			converted[fmt.Sprint(key)] = toJSONValue(item)
		}
		return converted
	case []any:
		for i, item := range value {
			value[i] = toJSONValue(item)
		}
		return value
	default:
		return value
	}
}

func formatSchemaError(err error) error {
	var validationErr *jsonschema.ValidationError
	if !errors.As(err, &validationErr) {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	var messages []string
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				walk(cause)
			}
			return
		}
		location := e.InstanceLocation
		if location == "" {
			location = "/"
		}
		messages = append(messages, fmt.Sprintf("%s: %s", location, e.Message))
	}
	walk(validationErr)

	return fmt.Errorf("schema validation failed:\n  - %s", strings.Join(messages, "\n  - "))
}
