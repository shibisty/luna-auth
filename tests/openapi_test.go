package tests

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	apptest "fullstack-testing"

	"luna-auth/docs"
)

// specOperations reads "METHOD /path" of every operation in the OpenAPI
// document (paths at two spaces, methods at four).
func specOperations(t *testing.T, spec string) map[string]bool {
	t.Helper()
	pathRe := regexp.MustCompile(`^  (/\S*):\s*$`)
	methodRe := regexp.MustCompile(`^    (get|put|post|patch|delete):\s*$`)
	ops := map[string]bool{}
	inPaths, current := false, ""
	for _, line := range strings.Split(spec, "\n") {
		switch {
		case line == "paths:":
			inPaths = true
		case inPaths && line != "" && !strings.HasPrefix(line, " "):
			inPaths = false
		case inPaths:
			if m := pathRe.FindStringSubmatch(line); m != nil {
				current = m[1]
			} else if m := methodRe.FindStringSubmatch(line); m != nil && current != "" {
				ops[strings.ToUpper(m[1])+" "+current] = true
			}
		}
	}
	if len(ops) == 0 {
		t.Fatal("no operations found in the OpenAPI document")
	}
	return ops
}

// TestOpenAPIMatchesRoutes keeps docs/openapi.yaml and the routes in step:
// every API route is documented and every documented operation exists.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	app := newApp(t, fakeDashboard(t, nil))
	if _, err := app.Handler(); err != nil {
		t.Fatal(err)
	}
	documented := specOperations(t, string(docs.OpenAPI))
	routes := map[string]bool{}
	for _, r := range app.Router.Routes() {
		if r.Path == "/openapi.yaml" || r.Path == "/docs" || r.Method == "HEAD" {
			continue
		}
		routes[r.Method+" "+r.Path] = true
	}
	var missing, extra []string
	for op := range routes {
		if !documented[op] {
			missing = append(missing, op)
		}
	}
	for op := range documented {
		if !routes[op] {
			extra = append(extra, op)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("docs/openapi.yaml is out of step with the routes:\n  routes not documented: %v\n  documented, no route: %v", missing, extra)
	}
}

func TestDocsServed(t *testing.T) {
	c := apptest.New(t, newApp(t, fakeDashboard(t, nil)))
	c.Get("/openapi.yaml").AssertOK().AssertHeader("Content-Type", "application/yaml; charset=utf-8").AssertSee("openapi: 3.1.0", "/v1/sessions")
	c.Get("/docs").AssertNotFound() // API_DOCS is off

	t.Setenv("API_DOCS", "true")
	c = apptest.New(t, newApp(t, fakeDashboard(t, nil)))
	c.Get("/docs").AssertOK().AssertSee("swagger-ui", "/openapi.yaml")
}
