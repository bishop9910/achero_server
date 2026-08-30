// Command openapi-check performs a lightweight structural validation of the
// OpenAPI document. It verifies the document parses as YAML and carries the
// fields required by the OpenAPI 3.x specification; it is not a full semantic
// linter. For stricter validation use `npx @redocly/cli lint openapi.yaml`.
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	path := "openapi.yaml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	b, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		fail("%s: invalid YAML: %v", path, err)
	}

	ver, _ := doc["openapi"].(string)
	if ver == "" {
		fail("%s: missing 'openapi' version", path)
	}

	info, ok := doc["info"].(map[string]any)
	if !ok {
		fail("%s: missing 'info' object", path)
	}
	title, _ := info["title"].(string)
	version, _ := info["version"].(string)
	if title == "" || version == "" {
		fail("%s: 'info.title' and 'info.version' are required", path)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		fail("%s: 'paths' must be a non-empty object", path)
	}

	ops := 0
	for p, item := range paths {
		pi, ok := item.(map[string]any)
		if !ok {
			fail("%s: path item %q is not an object", path, p)
		}
		pathOps := 0
		for k := range pi {
			if isHTTPMethod(k) {
				pathOps++
			}
		}
		if pathOps == 0 {
			fail("%s: path %q declares no operations", path, p)
		}
		ops += pathOps
	}

	fmt.Printf("%s: OK  openapi=%s  title=%q  version=%q  paths=%d  operations=%d\n",
		path, ver, title, version, len(paths), ops)
	fmt.Println("preview: https://editor.swagger.io/  or  npx @redocly/cli preview-docs openapi.yaml")
}

func isHTTPMethod(s string) bool {
	switch s {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return true
	}
	return false
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "openapi-check: "+format+"\n", args...)
	os.Exit(1)
}
