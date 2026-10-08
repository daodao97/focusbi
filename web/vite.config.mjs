import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import path from 'path'
import fs from 'fs'

// 把仓库 docs/ 下的 markdown 作为 /<name>.md 提供 (单一来源, 无需手动复制):
//   - dev: 中间件按需读取
//   - build: 写入 dist/<name>.md
// DOC_FILES: 路由名 -> 源文件路径。
const DOC_FILES = {
  'SYNTAX.md': path.resolve(__dirname, '../docs/SYNTAX.md'), // 报表模板语法
  'MCP.md': path.resolve(__dirname, '../docs/MCP.md')        // MCP 设置 (AI 工具中开发报表)
}

function reportDocPlugin() {
  return {
    name: 'report-doc',
    configureServer(server) {
      for (const [name, src] of Object.entries(DOC_FILES)) {
        server.middlewares.use('/' + name, (req, res) => {
          try {
            res.setHeader('Content-Type', 'text/markdown; charset=utf-8')
            res.end(fs.readFileSync(src, 'utf-8'))
          } catch {
            res.statusCode = 404; res.end(name + ' not found')
          }
        })
      }
    },
    closeBundle() {
      for (const [name, src] of Object.entries(DOC_FILES)) {
        // 复制失败直接让构建失败 (而非静默 warn): 否则镜像里缺文档, 运行时才暴露 404。
        fs.copyFileSync(src, path.resolve(__dirname, 'dist/' + name))
      }
    }
  }
}

// 多页 (MPA):
//   index.html  -> 管理控制台 (报表列表 / 编辑 / 数据源)
//   view.html   -> 独立报表查看页 (类似 dataddy /open)
export default ({ mode }) => {
  const env = loadEnv(mode, process.cwd())
  const serverApi = env.VITE_SERVER_API || 'http://127.0.0.1:8099'

  return defineConfig({
    base: './',
    resolve: {
      alias: { '@': path.resolve(__dirname, 'src') }
    },
    plugins: [
      vue(),
      AutoImport({ imports: ['vue', 'vue-router'], resolvers: [ElementPlusResolver()] }),
      Components({ resolvers: [ElementPlusResolver()] }),
      reportDocPlugin()
    ],
    server: {
      port: 3001,
      proxy: {
        '/api': { target: serverApi, changeOrigin: true }
      }
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      // Monaco / ECharts 保持按路由加载，不进入控制台首屏依赖。
      chunkSizeWarningLimit: 3500,
      rollupOptions: {
        input: {
          main: path.resolve(__dirname, 'index.html'),
          view: path.resolve(__dirname, 'view.html')
        },
        output: {
          // 把重型库拆成独立 vendor chunk: 按需加载 + 浏览器长期缓存
          manualChunks(id) {
            // Vite 的预加载辅助代码被首屏和懒加载路由共用；若落进 Monaco
            // chunk，入口会反向依赖整个编辑器，导致登录页也下载它。
            if (id.includes('vite/preload-helper') || id.includes('commonjsHelpers') ||
                id.includes('plugin-vue:export-helper')) return 'runtime'
            if (id.includes('/node_modules/monaco-editor/')) return 'monaco'
            if (id.includes('/node_modules/echarts/') || id.includes('/node_modules/zrender/')) return 'echarts'
            if (id.includes('/node_modules/element-plus/') ||
                id.includes('/node_modules/@element-plus/icons-vue/')) return 'element-plus'
            if (id.includes('/node_modules/vue/') || id.includes('/node_modules/vue-router/') ||
                id.includes('/node_modules/@vue/')) return 'vue'
          }
        }
      }
    }
  })
}
