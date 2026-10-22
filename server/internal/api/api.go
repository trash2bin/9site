// Package api контракт JSON API: типы ответа, спека и её отдача. Форма
// ответа объявлена здесь, остальное (сборка спеки, TS-типы) идёт за ней.
package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/invopop/jsonschema"
)

// Пути API записаны один раз: их читают роутер (album.Register), спека и
// тесты. Смена пути = правка здесь, остальное подтянется.
const (
	// PhotosPath JSON витрины: единственный вызов фронта.
	PhotosPath = "/api/photos"
	// SpecPath сама спека, отдаётся за логином.
	SpecPath = "/vault/openapi.json"
	// DeletePhotoPath POST удаления кадра, защищён сессией и CSRF.
	DeletePhotoPath = "/vault/album/delete"
	// FilesPath префикс файлов загрузок: он же в url записей.
	FilesPath = "/photos/"
)

// Photo снимок в JSON: карточке и фильтру нужны только эти поля.
type Photo struct {
	ID     int64    `json:"id" jsonschema:"description=идентификатор записи для действий владельца"`
	Name   string   `json:"name" jsonschema:"description=подпись кадра"`
	URL    string   `json:"url" jsonschema:"description=путь файла от корня сайта"`
	Album  string   `json:"album" jsonschema:"description=слаг раздела"`
	Width  int      `json:"width" jsonschema:"description=ширина файла в px"`
	Height int      `json:"height" jsonschema:"description=высота файла в px"`
	Author string   `json:"author" jsonschema:"description=логин загрузившего или system для засева"`
	Tags   []string `json:"tags" jsonschema:"description=теги кадра по алфавиту"`
}

// Section раздел витрины: слаг для фильтра и подпись для интерфейса.
type Section struct {
	Slug  string `json:"slug" jsonschema:"description=слаг раздела из album.Sections"`
	Label string `json:"label" jsonschema:"description=подпись для интерфейса"`
}

// Viewer текущий пользователь: административные действия доступны только администратору.
type Viewer struct {
	Admin bool   `json:"admin"`
	CSRF  string `json:"csrf"`
}

// PhotosResponse ответ витрины: снимки, разделы, теги и права текущей сессии.
type PhotosResponse struct {
	Photos []Photo   `json:"photos"`
	Albums []Section `json:"albums"`
	Tags   []string  `json:"tags"`
	Viewer Viewer    `json:"viewer"`
}

// Спека: каркас объявлен типами, схемы приходят из структур выше.

type document struct {
	OpenAPI    string              `json:"openapi"`
	Info       info                `json:"info"`
	Paths      map[string]pathItem `json:"paths"`
	Components components          `json:"components"`
}

type info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type pathItem struct {
	Get  *operation `json:"get,omitempty"`
	Post *operation `json:"post,omitempty"`
}

type operation struct {
	OperationID string              `json:"operationId"`
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	RequestBody *requestBody        `json:"requestBody,omitempty"`
	Responses   map[string]response `json:"responses"`
}

type requestBody struct {
	Required bool                     `json:"required"`
	Content  map[string]formMediaType `json:"content"`
}

type formMediaType struct {
	Schema formSchema `json:"schema"`
}

type formSchema struct {
	Type       string               `json:"type"`
	Properties map[string]formField `json:"properties"`
	Required   []string             `json:"required"`
}

type formField struct {
	Type string `json:"type"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type mediaType struct {
	Schema map[string]string `json:"schema"`
}

type components struct {
	Schemas map[string]any `json:"schemas"`
}

// Spec собранная спека: правится тестом с флагом -update, не руками.
//
//go:embed openapi.json
var spec []byte

// Spec отдаёт документ целиком.
func Spec() []byte { return spec }

// BuildSpec собирает спеку заново: поле, добавленное в Photo, меняет документ
// без отдельной правки.
func BuildSpec() ([]byte, error) {
	doc := document{
		OpenAPI: "3.1.0",
		Info: info{
			Title:       "Ivan's Little Web API",
			Version:     "1",
			Description: "JSON API витрины и действие удаления для сессии администратора.",
		},
		Paths: map[string]pathItem{
			PhotosPath: {Get: &operation{
				OperationID: "photos",
				Summary:     "Снимки, разделы и теги витрины",
				Description: "Открыт всем, ответ не кэшируется (Cache-Control: no-store).",
				Responses: map[string]response{
					"200": {
						Description: "Весь альбом",
						Content:     map[string]mediaType{"application/json": {Schema: ref("PhotosResponse")}},
					},
					"500": {
						Description: "База не читается",
						Content:     map[string]mediaType{"text/plain": {Schema: map[string]string{"type": "string"}}},
					},
				},
			}},
			DeletePhotoPath: {Post: &operation{
				OperationID: "deletePhoto",
				Summary:     "Удалить кадр",
				Description: "Доступно администратору; нужен CSRF-токен из photos.viewer.csrf. Чужой кадр обычному пользователю запрещён.",
				RequestBody: &requestBody{
					Required: true,
					Content: map[string]formMediaType{"application/x-www-form-urlencoded": {Schema: formSchema{
						Type: "object", Properties: map[string]formField{"csrf": {Type: "string"}, "id": {Type: "integer"}}, Required: []string{"csrf", "id"},
					}}},
				},
				Responses: map[string]response{
					"303": {Description: "Кадр удалён, переход на страницу альбома"},
					"403": {Description: "Недостаточно прав или неверный CSRF-токен"},
					"404": {Description: "Кадр не найден"},
				},
			}},
		},

		Components: components{Schemas: schemas()},
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	// Схемы ссылаются на определения, а в OpenAPI они лежат в components.
	return bytes.ReplaceAll(raw, []byte("#/$defs/"), []byte("#/components/schemas/")), nil
}

// schemas выводит компоненты из типов ответа, служебные поля отражателя не нужны.
func schemas() map[string]any {
	root := (&jsonschema.Reflector{}).Reflect(&PhotosResponse{})
	out := make(map[string]any, len(root.Definitions))
	for name, def := range root.Definitions {
		def.Version, def.ID = "", ""
		out[name] = def
	}
	return out
}

// ref ссылка на схему в components.
func ref(name string) map[string]string {
	return map[string]string{"$ref": "#/components/schemas/" + name}
}

// Handler отдаёт спеку: срез вешает его за проверкой сессии, снаружи без входа
// документа нет.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(spec)
	})
}
