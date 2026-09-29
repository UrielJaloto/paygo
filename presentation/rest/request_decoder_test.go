package rest_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/UrielJaloto/paygo/presentation/rest"
	"github.com/gin-gonic/gin"
)

type decodedBody struct {
	Name string `json:"name"`
}

func decodeRequest(contentType, body string) (decodedBody, error) {
	var decoded decodedBody
	var decodeErr error
	engine := gin.New()
	engine.POST("/decode", func(c *gin.Context) { decodeErr = rest.DecodeJSON(c, &decoded) })
	request := httptest.NewRequest(http.MethodPost, "/decode", strings.NewReader(body))
	request.Header.Set("Content-Type", contentType)
	send(engine, request)
	return decoded, decodeErr
}

func TestDecodeJSON_InvalidBody_ReturnsTypedError(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		want        error
	}{
		{"malformed", "application/json", "{", rest.ErrMalformedRequest},
		{"empty", "application/json", "", rest.ErrMalformedRequest},
		{"unknown field", "application/json", `{"x":1}`, rest.ErrMalformedRequest},
		{"wrong type", "application/json", `{"name":1}`, rest.ErrMalformedRequest},
		{"trailing data", "application/json", `{}{}`, rest.ErrMalformedRequest},
		{"too large", "application/json", `{"name":"` + strings.Repeat("a", 64<<10) + `"}`, rest.ErrPayloadTooLarge},
		{"too large after value", "application/json", `{}` + strings.Repeat(" ", 64<<10), rest.ErrPayloadTooLarge},
		{"wrong content type", "text/plain", `{}`, rest.ErrUnsupportedMediaType},
		{"missing content type", "", `{}`, rest.ErrUnsupportedMediaType},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := decodeRequest(testCase.contentType, testCase.body); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestDecodeJSON_ValidBody_FillsDestination(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{"plain", "application/json", `{"name":"ana"}`},
		{"with charset", "application/json; charset=utf-8", `{"name":"ana"}`},
		{"at the size limit", "application/json", `{"name":"ana"}` + strings.Repeat(" ", 64<<10-14)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := decodeRequest(testCase.contentType, testCase.body)
			if err != nil || got.Name != "ana" {
				t.Fatalf("got %+v, error = %v", got, err)
			}
		})
	}
}
