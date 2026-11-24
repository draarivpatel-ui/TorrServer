package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"silo/internal/log"
	"silo/internal/plugin"
	"silo/internal/user"

	"github.com/gin-gonic/gin"
)

// UISource отдаёт страницы интерфейса плагинов.
type UISource interface {
	UIPages(pluginID string) []plugin.UIPageInfo
	UISchema(pluginID, page string, ctx plugin.UIContext) ([]byte, error)
}

type RouteEntry struct {
	Route   string
	VFS     fs.FS
	VFSPath string
	Handler gin.HandlerFunc
	Method  string
}
type PluginRouter struct {
	exactRoutes    map[string]RouteEntry
	prefixRoutes   []RouteEntry
	wildcardRoutes []RouteEntry
}

type PluginsRouter struct {
	mu            sync.RWMutex
	activeThemeID string
	routers       map[string]*PluginRouter
	uiSource      UISource
	userSvc       *user.Service
}

func NewPluginsRouter() *PluginsRouter {
	return &PluginsRouter{
		routers: make(map[string]*PluginRouter),
	}
}

// SetUISource связывает роутер с источником страниц интерфейса.
func (pr *PluginsRouter) SetUISource(src UISource) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.uiSource = src
}

// SetUserService связывает роутер с сервисом пользователей для схемы.
func (pr *PluginsRouter) SetUserService(svc *user.Service) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.userSvc = svc
}

func (pr *PluginsRouter) Reset() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.activeThemeID = ""
	pr.routers = make(map[string]*PluginRouter)
	log.Debug("[Web] Plugins router reset")
}

func (pr *PluginsRouter) SetActiveTheme(pluginID string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.activeThemeID = pluginID
	log.Infof("[Web] Active theme UI set to plugin '%s'", pluginID)
}

func (pr *PluginsRouter) getOrCreatePluginRouter(pluginID string) *PluginRouter {
	if r, exists := pr.routers[pluginID]; exists {
		return r
	}
	r := &PluginRouter{
		exactRoutes:    make(map[string]RouteEntry),
		prefixRoutes:   make([]RouteEntry, 0),
		wildcardRoutes: make([]RouteEntry, 0),
	}
	pr.routers[pluginID] = r
	return r
}

func (pr *PluginsRouter) AddJSHandler(pluginID, method, route string, handler gin.HandlerFunc) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	r := pr.getOrCreatePluginRouter(pluginID)

	if idx := strings.Index(route, "/*"); idx >= 0 {
		prefix := route[:idx]
		if prefix == "" {
			prefix = "/"
		}
		r.wildcardRoutes = append(r.wildcardRoutes, RouteEntry{
			Route:   prefix,
			Method:  method,
			Handler: handler,
		})
		log.Debugf("[Web] Plugin '%s' registered JS handler %s %s", pluginID, method, route)
		return
	}

	key := method + ":" + path.Clean(route)
	r.exactRoutes[key] = RouteEntry{Route: route, Handler: handler}
	log.Debugf("[Web] Plugin '%s' registered JS handler for %s %s", pluginID, method, route)
}

func (pr *PluginsRouter) AddStaticFile(pluginID, route, vfsPath string, vfs fs.FS) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	r := pr.getOrCreatePluginRouter(pluginID)
	key := "GET:" + path.Clean(route)
	r.exactRoutes[key] = RouteEntry{Route: route, VFS: vfs, VFSPath: vfsPath}
	log.Debugf("[Web] Plugin '%s' registered static file %s -> %s", pluginID, route, vfsPath)
}

func (pr *PluginsRouter) AddStaticDir(pluginID, route, vfsDir string, vfs fs.FS) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	r := pr.getOrCreatePluginRouter(pluginID)
	cleanRoute := strings.TrimSuffix(route, "/*")
	r.prefixRoutes = append(r.prefixRoutes, RouteEntry{Route: cleanRoute, VFS: vfs, VFSPath: vfsDir})
	log.Debugf("[Web] Plugin '%s' registered static dir %s/* -> %s/", pluginID, cleanRoute, vfsDir)
}

func (pr *PluginsRouter) HandleRoot(c *gin.Context) {
	pr.mu.RLock()
	themeID := pr.activeThemeID
	pr.mu.RUnlock()

	if themeID != "" {
		c.Redirect(http.StatusTemporaryRedirect, "/plugins/"+themeID+"/index.html")
		return
	}
	c.String(http.StatusOK, "Silo Core API is running. No theme UI plugin installed.")
}

func (pr *PluginsRouter) Dispatch(c *gin.Context) {
	pluginID := c.Param("id")
	reqPath := path.Clean(c.Param("path"))
	if reqPath == "" {
		reqPath = "/"
	}

	// Схема интерфейса: /ui/ отдаёт JSON клиентам и оболочку браузеру
	if reqPath == "/ui" || strings.HasPrefix(reqPath, "/ui/") {
		if wantsHTML(c.Request) && !wantsUISchema(c.Request) {
			if !pr.hasUIPages(pluginID) {
				c.JSON(http.StatusNotFound, gin.H{"error": "ui page not found"})
				return
			}
			pr.serveUIShell(c)
			return
		}
		page := strings.Trim(strings.TrimPrefix(reqPath, "/ui"), "/")
		pr.serveUISchema(c, pluginID, page)
		return
	}
	if reqPath == "/" && wantsUISchema(c.Request) {
		pr.serveUISchema(c, pluginID, c.Query("page"))
		return
	}

	key := c.Request.Method + ":" + reqPath

	pr.mu.RLock()
	pluginR, exists := pr.routers[pluginID]
	pr.mu.RUnlock()

	if exists {
		if entry, found := pluginR.exactRoutes[key]; found {
			pr.serveEntry(c, entry, entry.VFSPath)
			return
		}

		for i := range pluginR.wildcardRoutes {
			entry := &pluginR.wildcardRoutes[i]
			if entry.Method != c.Request.Method {
				continue
			}
			if entry.Route == "/" || reqPath == entry.Route || strings.HasPrefix(reqPath, entry.Route+"/") {
				pr.serveEntry(c, *entry, "")
				return
			}
		}

		if c.Request.Method == http.MethodGet {
			var bestMatch *RouteEntry
			for i := range pluginR.prefixRoutes {
				entry := &pluginR.prefixRoutes[i]
				if entry.Route == "/" || reqPath == entry.Route || strings.HasPrefix(reqPath, entry.Route+"/") {
					if bestMatch == nil || len(entry.Route) > len(bestMatch.Route) {
						bestMatch = entry
					}
				}
			}

			if bestMatch != nil {
				relPath := strings.TrimPrefix(reqPath, bestMatch.Route)
				relPath = strings.TrimPrefix(relPath, "/")
				targetFile := path.Join(bestMatch.VFSPath, relPath)
				pr.serveEntry(c, *bestMatch, targetFile)
				return
			}
		}
	}

	// У плагина нет своей страницы, но есть схема: отдаём оболочку ядра
	if reqPath == "/" && pr.hasUIPages(pluginID) {
		pr.serveUIShell(c)
		return
	}

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin not found or has no routes"})
		return
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "route not found in plugin"})
}

// serveUISchema отдаёт схему страницы интерфейса.
func (pr *PluginsRouter) serveUISchema(c *gin.Context, pluginID, page string) {
	pr.mu.RLock()
	source := pr.uiSource
	pr.mu.RUnlock()

	if source == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ui is not available"})
		return
	}

	if page == "" {
		page = c.Query("page")
	}
	if page == "" {
		page = "main"
	}
	if !plugin.ValidUIPageID(page) {
		c.JSON(http.StatusNotFound, gin.H{"error": "invalid ui page"})
		return
	}

	if v := requestedUIVersion(c.Request); v != "" && v != strconv.Itoa(plugin.UIVersion) {
		c.JSON(http.StatusNotAcceptable, gin.H{
			"error":     "unsupported ui version",
			"supported": plugin.UIVersion,
		})
		return
	}

	ctx := plugin.UIContext{
		User:  pr.currentUser(c.Request),
		Page:  page,
		Lang:  uiLang(c.Request),
		Query: uiQuery(c.Request),
	}

	data, err := source.UISchema(pluginID, page, ctx)
	if err != nil {
		switch {
		case errors.Is(err, plugin.ErrUIPageNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "ui page not found"})
		case errors.Is(err, plugin.ErrUIPageForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": "ui page forbidden"})
		case errors.Is(err, plugin.ErrUISchemaInvalid):
			log.Errorf("[Web] Plugin '%s' returned invalid ui schema: %v", pluginID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid ui schema"})
		default:
			log.Errorf("[Web] Failed to render ui page '%s' of '%s': %v", page, pluginID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ui render failed"})
		}
		return
	}

	c.Header("Content-Type", plugin.UIMediaType+"; charset=utf-8")
	c.Header("Vary", "Accept")
	c.Header("X-Silo-UI-Version", strconv.Itoa(plugin.UIVersion))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, plugin.UIMediaType+"; charset=utf-8", data)
}

// hasUIPages сообщает, есть ли у плагина страницы интерфейса.
func (pr *PluginsRouter) hasUIPages(pluginID string) bool {
	pr.mu.RLock()
	source := pr.uiSource
	pr.mu.RUnlock()

	if source == nil {
		return false
	}
	return len(source.UIPages(pluginID)) > 0
}

// currentUser возвращает пользователя по токену, если он передан.
func (pr *PluginsRouter) currentUser(r *http.Request) *user.User {
	pr.mu.RLock()
	svc := pr.userSvc
	pr.mu.RUnlock()

	if svc == nil {
		return nil
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if token == "" {
		if cookie, err := r.Cookie("silo_token"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		return nil
	}

	u, err := svc.Authenticate(token)
	if err != nil {
		return nil
	}
	return u
}

// uiLang выбирает язык из параметра или заголовка.
func uiLang(r *http.Request) string {
	if v := strings.TrimSpace(r.URL.Query().Get("lang")); v != "" {
		return v
	}

	header := r.Header.Get("Accept-Language")
	if header == "" {
		return "en"
	}
	for _, part := range strings.Split(header, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if tag == "" {
			continue
		}
		if i := strings.IndexAny(tag, "-_"); i > 0 {
			tag = tag[:i]
		}
		return strings.ToLower(tag)
	}
	return "en"
}

// uiQuery отдаёт плагину параметры запроса.
func uiQuery(r *http.Request) map[string]string {
	out := make(map[string]string)
	for k, vals := range r.URL.Query() {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

// wantsUISchema проверяет, запрошена ли схема интерфейса.
func wantsUISchema(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		media := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.EqualFold(media, plugin.UIMediaType) {
			return true
		}
	}
	return false
}

// wantsHTML проверяет, что клиент просит страницу.
func wantsHTML(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		media := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.EqualFold(media, "text/html") {
			return true
		}
	}
	return false
}

// requestedUIVersion возвращает версию из Accept, если она указана.
func requestedUIVersion(r *http.Request) string {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		segments := strings.Split(part, ";")
		if !strings.EqualFold(strings.TrimSpace(segments[0]), plugin.UIMediaType) {
			continue
		}
		for _, s := range segments[1:] {
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "v=") {
				return strings.TrimPrefix(s, "v=")
			}
			if strings.HasPrefix(s, "version=") {
				return strings.TrimPrefix(s, "version=")
			}
		}
	}
	return ""
}

// uiShellHTML хранит прочитанную оболочку страницы схемы.
var (
	uiShellOnce sync.Once
	uiShellData []byte
	uiShellErr  error
)

// serveUIShell отдаёт оболочку, которая загружает схему и рисует её.
func (pr *PluginsRouter) serveUIShell(c *gin.Context) {
	uiShellOnce.Do(func() {
		uiShellData, uiShellErr = fs.ReadFile(staticFS, "static/ui.html")
	})
	if uiShellErr != nil {
		log.Errorf("[Web] Failed to read ui shell: %v", uiShellErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ui shell is not available"})
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", uiShellData)
}

func (pr *PluginsRouter) serveEntry(c *gin.Context, entry RouteEntry, targetFile string) {
	if entry.Handler != nil {
		entry.Handler(c)
		return
	}

	cleanFile := strings.TrimPrefix(targetFile, "/")
	data, err := fs.ReadFile(entry.VFS, cleanFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found in VFS"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		}
		return
	}

	contentType := detectContentType(cleanFile, data)
	c.Data(http.StatusOK, contentType, data)
}

// RemovePlugin полностью удаляет все зарегистрированные маршруты конкретного плагина
func (pr *PluginsRouter) RemovePlugin(pluginID string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	delete(pr.routers, pluginID)
	if pr.activeThemeID == pluginID {
		pr.activeThemeID = ""
	}
	log.Debugf("[Web] Removed all routes for plugin '%s'", pluginID)
}

// ActiveThemeID возвращает ID плагина, установленного темой главной страницы
func (pr *PluginsRouter) ActiveThemeID() string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.activeThemeID
}

func detectContentType(filePath string, data []byte) string {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webmanifest":
		return "application/manifest+json"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json"
	}

	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	return ct
}
