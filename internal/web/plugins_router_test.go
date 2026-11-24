package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"silo/internal/plugin"

	"github.com/gin-gonic/gin"
)

// fakeUISource отдаёт заранее заданную схему.
type fakeUISource struct {
	pages    []plugin.UIPageInfo
	data     []byte
	err      error
	lastPage string
}

func (f *fakeUISource) UIPages(string) []plugin.UIPageInfo { return f.pages }

func (f *fakeUISource) UISchema(_, page string, _ plugin.UIContext) ([]byte, error) {
	f.lastPage = page
	return f.data, f.err
}

// newUIRouter собирает роутер плагинов с заглушкой источника схемы.
func newUIRouter(src *fakeUISource) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := NewPluginsRouter()
	router.SetUISource(src)

	engine := gin.New()
	engine.Any("/plugins/:id/*path", router.Dispatch)
	return engine
}

// TestPluginUISchemaJSON проверяет выдачу схемы клиенту.
func TestPluginUISchemaJSON(t *testing.T) {
	src := &fakeUISource{
		pages: []plugin.UIPageInfo{{ID: "main"}},
		data:  []byte(`{"version":1,"type":"screen","children":[]}`),
	}
	engine := newUIRouter(src)

	req := httptest.NewRequest(http.MethodGet, "/plugins/tester/ui/main", nil)
	req.Header.Set("Accept", plugin.UIMediaType)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, plugin.UIMediaType) {
		t.Errorf("content type = %q", ct)
	}
	if got := rec.Header().Get("X-Silo-UI-Version"); got != "1" {
		t.Errorf("version header = %q", got)
	}
	if src.lastPage != "main" {
		t.Errorf("page = %q, want main", src.lastPage)
	}
}

// TestPluginUISchemaHTML проверяет оболочку для браузера.
func TestPluginUISchemaHTML(t *testing.T) {
	src := &fakeUISource{pages: []plugin.UIPageInfo{{ID: "main"}}}
	engine := newUIRouter(src)

	req := httptest.NewRequest(http.MethodGet, "/plugins/tester/ui/main", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `id="ui-root"`) {
		t.Errorf("shell does not contain the root node")
	}
}

// TestPluginUISchemaVersion проверяет отказ неподдерживаемой версии.
func TestPluginUISchemaVersion(t *testing.T) {
	src := &fakeUISource{pages: []plugin.UIPageInfo{{ID: "main"}}, data: []byte(`{}`)}
	engine := newUIRouter(src)

	req := httptest.NewRequest(http.MethodGet, "/plugins/tester/ui/main", nil)
	req.Header.Set("Accept", plugin.UIMediaType+"; v=2")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want 406", rec.Code)
	}
}

// TestPluginUISchemaErrors проверяет коды ошибок источника.
func TestPluginUISchemaErrors(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{plugin.ErrUIPageNotFound, http.StatusNotFound},
		{plugin.ErrUIPageForbidden, http.StatusForbidden},
		{plugin.ErrUISchemaInvalid, http.StatusInternalServerError},
		{errors.New("boom"), http.StatusInternalServerError},
	}

	for _, c := range cases {
		src := &fakeUISource{pages: []plugin.UIPageInfo{{ID: "main"}}, err: c.err}
		engine := newUIRouter(src)

		req := httptest.NewRequest(http.MethodGet, "/plugins/tester/ui/main", nil)
		req.Header.Set("Accept", plugin.UIMediaType)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != c.want {
			t.Errorf("error %v: status = %d, want %d", c.err, rec.Code, c.want)
		}
	}
}

// TestPluginUIRootShell проверяет корень плагина без своей страницы.
func TestPluginUIRootShell(t *testing.T) {
	src := &fakeUISource{pages: []plugin.UIPageInfo{{ID: "main"}}}
	engine := newUIRouter(src)

	req := httptest.NewRequest(http.MethodGet, "/plugins/tester/", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("root must return the shell")
	}
}
