// Package api_test тесты контракта: спека собирается из типов и не отстаёт.
package api_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/api"
)

// update перегенерация golden-файла: go test ./server/internal/api/tests -run TestSpecFresh -update
var update = flag.Bool("update", false, "перегенерировать спеку")

// TestSpecFresh: файл спеки равен тому, что собирается из Go-типов. Поменяли
// поле ответа и забыли перегенерацию, красный тест говорит об этом сразу.
func TestSpecFresh(t *testing.T) {
	built, err := api.BuildSpec()
	if err != nil {
		t.Fatalf("спека не собирается: %v", err)
	}
	if *update {
		if err := os.WriteFile("../openapi.json", built, 0o644); err != nil {
			t.Fatalf("спека не пишется: %v", err)
		}
		return
	}
	if !bytes.Equal(built, api.Spec()) {
		t.Fatal("спека устарела: go test ./server/internal/api/tests -run TestSpecFresh -update")
	}
}

// TestSpecDescribesResponse: документ описывает ровно те поля, что уходят в JSON.
func TestSpecDescribesResponse(t *testing.T) {
	spec := api.Spec()

	var doc struct {
		OpenAPI    string `json:"openapi"`
		Components struct {
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("спека не JSON: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi %q", doc.OpenAPI)
	}

	response, ok := doc.Components.Schemas["PhotosResponse"]
	if !ok {
		t.Fatalf("нет схемы PhotosResponse: %v", doc.Components.Schemas)
	}
	for _, field := range []string{"photos", "albums", "tags", "viewer"} {
		if !contains(response.Required, field) {
			t.Fatalf("поле %q не обязательное: %v", field, response.Required)
		}
	}

	photo, ok := doc.Components.Schemas["Photo"]
	if !ok {
		t.Fatal("нет схемы Photo")
	}
	for _, field := range []string{"id", "name", "url", "album", "width", "height", "author", "tags"} {
		if _, ok := photo.Properties[field]; !ok {
			t.Fatalf("в Photo нет поля %q", field)
		}
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
