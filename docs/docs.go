// Package docs is the API description: the OpenAPI document (served at
// /openapi.yaml) and Swagger UI over it (at /docs, when API_DOCS=true).
package docs

import (
	_ "embed"
	"html/template"
	"net/http"

	"fullstack/web"
)

// OpenAPI is the OpenAPI 3.1 document of the API.
//
//go:embed openapi.yaml
var OpenAPI []byte

// Register serves the document at specPath and, when ui is true, Swagger UI
// at uiPath. Swagger UI's scripts come from the jsDelivr CDN, so the page
// needs the internet; the document does not.
func Register(r *web.Router, specPath, uiPath, title string, ui bool) {
	r.Get(specPath, func(c *web.Ctx) error {
		h := c.W.Header()
		h.Set("Content-Type", "application/yaml; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		h.Set("Access-Control-Allow-Origin", "*") // tools like editor.swagger.io may read it
		_, err := c.W.Write(OpenAPI)
		return err
	}).Name("docs.openapi")
	if !ui {
		return
	}
	r.Get(uiPath, func(c *web.Ctx) error {
		c.W.Header().Set("Content-Type", "text/html; charset=utf-8")
		c.W.WriteHeader(http.StatusOK)
		return page.Execute(c.W, map[string]string{"Title": title, "Spec": specPath})
	}).Name("docs.ui")
}

var page = template.Must(template.New("docs").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.ui = SwaggerUIBundle({ url: {{.Spec}}, dom_id: "#swagger-ui", persistAuthorization: true });
  </script>
</body>
</html>
`))
