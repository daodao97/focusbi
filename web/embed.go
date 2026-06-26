// Package web 内嵌前端构建产物 (Vue3 + Vite, 多页)。
// 运行 `make web` 或 `cd web && pnpm build` 生成 dist/。
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
