package generation

import (
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
)

func GenerateSchema[T any]() *jsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
	}
	var v T
	return reflector.Reflect(v)
}

type GenerationClient struct {
	client openai.Client
}

func NewClient(apiKey string) *GenerationClient {
	return &GenerationClient{
		client: openai.NewClient(option.WithAPIKey(apiKey)),
	}

}
