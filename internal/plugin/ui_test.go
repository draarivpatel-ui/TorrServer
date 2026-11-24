package plugin

import (
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"silo/internal/config"
	"silo/internal/database"
	"silo/internal/user"
)

// uiDoc собирает минимальный документ интерфейса.
func uiDoc(children ...any) map[string]any {
	return map[string]any{
		"version": 1,
		"type":    "screen",
		"props":   map[string]any{"title_key": "plugin.title"},
		"children": append([]any{
			map[string]any{
				"type":  "field",
				"id":    "text",
				"props": map[string]any{"input": "text", "value": "hi"},
			},
		}, children...),
	}
}

// TestNormalizeUISchemaNormalizesActionPath проверяет путь действия.
func TestNormalizeUISchemaNormalizesActionPath(t *testing.T) {
	doc := uiDoc()
	doc["actions"] = map[string]any{
		"save": map[string]any{
			"type":   "submit",
			"method": "post",
			"path":   "api/save",
			"body":   map[string]any{"text": "$text"},
		},
	}

	out, err := normalizeUISchema("tester", doc)
	if err != nil {
		t.Fatalf("normalizeUISchema failed: %v", err)
	}

	actions := out["actions"].(map[string]any)
	save := actions["save"].(map[string]any)
	if save["path"] != "/plugins/tester/api/save" {
		t.Errorf("path = %v, want /plugins/tester/api/save", save["path"])
	}
	if save["method"] != "POST" {
		t.Errorf("method = %v, want POST", save["method"])
	}
}

// TestNormalizeUISchemaRejectsBadActions проверяет опасные адреса действий.
func TestNormalizeUISchemaRejectsBadActions(t *testing.T) {
	cases := []string{
		"https://evil.example/api",
		"//evil.example/api",
		"../escape",
		"api/save?x=1",
	}

	for _, raw := range cases {
		doc := uiDoc()
		doc["actions"] = map[string]any{
			"save": map[string]any{
				"type":   "submit",
				"method": "POST",
				"path":   raw,
			},
		}
		if _, err := normalizeUISchema("tester", doc); err == nil {
			t.Errorf("path %q must be rejected", raw)
		}
	}
}

// TestNormalizeUISchemaRejectsUnknownComponent проверяет неизвестный тип.
func TestNormalizeUISchemaRejectsUnknownComponent(t *testing.T) {
	doc := uiDoc(map[string]any{"type": "unknown"})
	if _, err := normalizeUISchema("tester", doc); err == nil {
		t.Fatal("unknown component must be rejected")
	}
}

// TestNormalizeUISchemaRejectsDuplicateID проверяет повтор идентификатора.
func TestNormalizeUISchemaRejectsDuplicateID(t *testing.T) {
	doc := uiDoc()
	doc["children"] = []any{
		map[string]any{"type": "field", "id": "text", "props": map[string]any{"input": "text"}},
		map[string]any{"type": "field", "id": "text", "props": map[string]any{"input": "text"}},
	}
	if _, err := normalizeUISchema("tester", doc); err == nil {
		t.Fatal("duplicate id must be rejected")
	}
}

// TestNormalizeUISchemaRejectsFieldWithoutInput проверяет поле без типа.
func TestNormalizeUISchemaRejectsFieldWithoutInput(t *testing.T) {
	doc := uiDoc()
	doc["children"] = []any{
		map[string]any{"type": "field", "id": "text", "props": map[string]any{"value": "hi"}},
	}
	if _, err := normalizeUISchema("tester", doc); err == nil {
		t.Fatal("field without input must be rejected")
	}
}

// TestNormalizeUISchemaRejectsRoot проверяет, что корень - screen.
func TestNormalizeUISchemaRejectsRoot(t *testing.T) {
	doc := uiDoc()
	doc["type"] = "section"
	if _, err := normalizeUISchema("tester", doc); err == nil {
		t.Fatal("root must be screen")
	}
}

// TestNormalizeUISchemaRejectsVersion проверяет версию документа.
func TestNormalizeUISchemaRejectsVersion(t *testing.T) {
	doc := uiDoc()
	doc["version"] = 2
	if _, err := normalizeUISchema("tester", doc); err == nil {
		t.Fatal("unsupported version must be rejected")
	}
}

// TestNormalizeUISchemaURLs проверяет адреса картинок и ссылок.
func TestNormalizeUISchemaURLs(t *testing.T) {
	doc := uiDoc(
		map[string]any{"type": "media", "props": map[string]any{"src": "img/a.png"}},
		map[string]any{"type": "link", "props": map[string]any{"href": "/"}},
		map[string]any{"type": "text", "props": map[string]any{"value": "x"}},
	)

	out, err := normalizeUISchema("tester", doc)
	if err != nil {
		t.Fatalf("normalizeUISchema failed: %v", err)
	}

	children := out["children"].([]any)
	media := children[1].(map[string]any)
	if got := media["props"].(map[string]any)["src"]; got != "/plugins/tester/img/a.png" {
		t.Errorf("src = %v, want /plugins/tester/img/a.png", got)
	}

	link := children[2].(map[string]any)
	if got := link["props"].(map[string]any)["href"]; got != "/" {
		t.Errorf("href = %v, want /", got)
	}
}

// TestUIPageRegisteredFromJS проверяет регистрацию и вызов страницы.
func TestUIPageRegisteredFromJS(t *testing.T) {
	vfs := fstest.MapFS{
		"info.yaml": &fstest.MapFile{Data: []byte("id: \"tester\"\nname: \"Tester\"\nentry: \"service.js\"\n")},
		"service.js": &fstest.MapFile{Data: []byte(`
ts.ui.page("main", function (req) {
    return {
        version: 1,
        type: "screen",
        props: { title_key: "plugin.title" },
        actions: {
            save: { type: "submit", method: "POST", path: "api/save", body: { text: "$text" } }
        },
        children: [
            { type: "field", id: "text", props: { input: "text", value: "hi" } },
            { type: "button", props: { label: "Save", action: "save" } }
        ]
    };
});
`)},
	}

	manifest, err := LoadManifestFromFS(vfs)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}

	db, err := database.Open(filepath.Join(t.TempDir(), "ui.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	userSvc := user.NewService(user.NewStore(db), config.DefaultConfig())
	rt := NewJSRuntime("tester", manifest, vfs, newStubRegistrar(), db, nil, userSvc, NewI18nRegistry(), nil, nil, nil, nil)

	code, err := fs.ReadFile(vfs, "service.js")
	if err != nil {
		t.Fatalf("service.js: %v", err)
	}
	if err := rt.Execute(string(code)); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !rt.HasUIPage("main") {
		t.Fatal("page 'main' is not registered")
	}

	raw, err := rt.UISchema("main", UIContext{Page: "main"})
	if err != nil {
		t.Fatalf("UISchema: %v", err)
	}

	doc, err := normalizeUISchema("tester", raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	actions := doc["actions"].(map[string]any)
	save := actions["save"].(map[string]any)
	if save["path"] != "/plugins/tester/api/save" {
		t.Errorf("path = %v", save["path"])
	}
}

// TestUISchemaStaticPage проверяет статическую страницу из VFS.
func TestUISchemaStaticPage(t *testing.T) {
	vfs := fstest.MapFS{
		"info.yaml": &fstest.MapFile{Data: []byte("id: \"tester\"\nname: \"Tester\"\n")},
		"ui/main.json": &fstest.MapFile{Data: []byte(`{
			"version": 1,
			"type": "screen",
			"props": { "title_key": "plugin.title" },
			"children": [
				{ "type": "text", "props": { "value": "static" } }
			]
		}`)},
	}

	manifest, err := LoadManifestFromFS(vfs)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}

	db, err := database.Open(filepath.Join(t.TempDir(), "static.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	userSvc := user.NewService(user.NewStore(db), config.DefaultConfig())
	rt := NewJSRuntime("tester", manifest, vfs, newStubRegistrar(), db, nil, userSvc, NewI18nRegistry(), nil, nil, nil, nil)
	rt.RegisterUIPage(UIPageInfo{ID: "main"}, "ui/main.json")

	raw, err := rt.UISchema("main", UIContext{Page: "main"})
	if err != nil {
		t.Fatalf("UISchema: %v", err)
	}

	doc, err := normalizeUISchema("tester", raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if doc["type"] != "screen" {
		t.Errorf("type = %v", doc["type"])
	}
}
