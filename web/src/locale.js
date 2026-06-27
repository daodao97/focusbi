// 轻量本地化: 跟随浏览器语言 (zh-* -> 中文, 其余 -> 英文)。
// 只覆盖 Element Plus 内置文案 (日期选择器月份/今天/清空等) 与报表筛选控件的少量自定义串;
// 不引入 vue-i18n 框架 —— 报表名/过滤器 label 由作者在模板里写死, 无法按浏览器语言翻译。
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'

const isZh = (navigator.language || 'zh').toLowerCase().startsWith('zh')

// 传给 app.use(ElementPlus, { locale }), 让日期选择器等内置控件跟随语言。
export const elementLocale = isZh ? zhCn : en

const dict = {
  all: isZh ? '全部' : 'All',
  rangeSep: isZh ? '至' : 'to',
  start: isZh ? '开始' : 'Start',
  end: isZh ? '结束' : 'End',
  query: isZh ? '查询' : 'Query',
}

// t(key): 取筛选控件自定义文案; 语言在页面加载时定一次, 不需要响应式。
export const t = (k) => dict[k] || k
