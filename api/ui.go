package api

import (
	"io/fs"
	"net/http"
	"strings"

	"xproxy/web"

	"github.com/gin-gonic/gin"
)

// registerUI 挂载内嵌的前端 (Vue3 + Vite 多页):
//
//	/            -> 管理控制台 (dist/index.html)
//	/view.html   -> 独立报表查看页
//	/assets/*    -> 构建产物静态资源
func registerUI(e *gin.Engine) {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		// dist 缺失 (未构建前端) 时降级, 不阻断 API 启动
		e.GET("/", func(c *gin.Context) {
			c.String(http.StatusOK, "前端尚未构建, 请执行 `cd web && pnpm install && pnpm build`")
		})
		return
	}

	fileServer := http.FileServer(http.FS(dist))

	serve := func(c *gin.Context, name string) {
		data, err := fs.ReadFile(dist, name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	}

	e.GET("/", func(c *gin.Context) { serve(c, "index.html") })
	e.GET("/index.html", func(c *gin.Context) { serve(c, "index.html") })
	e.GET("/view.html", func(c *gin.Context) { serve(c, "view.html") })

	// 开发文档 (内嵌的 docs/*.md): SYNTAX 报表语法, MCP 在 AI 工具中开发报表
	serveMarkdown := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			data, err := fs.ReadFile(dist, name)
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			c.Data(http.StatusOK, "text/markdown; charset=utf-8", data)
		}
	}
	e.GET("/SYNTAX.md", serveMarkdown("SYNTAX.md"))
	e.GET("/MCP.md", serveMarkdown("MCP.md"))

	// 静态资源 (assets/ 及 favicon 等)
	e.GET("/assets/*filepath", gin.WrapH(fileServer))

	// 兜底: 其余 GET 非 /api 路径回退到控制台首页 (支持前端 hash 路由直接访问)
	e.NoRoute(func(c *gin.Context) {
		if c.Request.Method == http.MethodGet && !strings.HasPrefix(c.Request.URL.Path, "/api") {
			serve(c, "index.html")
			return
		}
		c.Status(http.StatusNotFound)
	})
}
