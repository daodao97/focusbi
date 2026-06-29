// 数值显示格式化: 与后端列配置 format 口径一致 (money/number/integer/percent)。
// 后端 rows 只放原始数值 (供图表/排序/汇总), 展示格式仅在前端渲染时套用, 避免污染数值。

// 千分位分组; 处理负号与小数。
export function addThousands(s) {
  const neg = s.startsWith('-')
  if (neg) s = s.slice(1)
  const [int, frac] = s.split('.')
  const grouped = int.replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  return (neg ? '-' : '') + grouped + (frac != null ? '.' + frac : '')
}

// fmtNumber 按 format 把数值格式化为展示字符串。非数值/无 format 时原样返回。
export function fmtNumber(v, format) {
  if (v == null || v === '') return v
  if (!format) return v
  const n = Number(String(v).replace(/,/g, ''))
  if (!Number.isFinite(n)) return v
  switch (String(format).toLowerCase()) {
    case 'money':
    case 'currency':
      return addThousands(n.toFixed(2))
    case 'number':
      return addThousands(String(n))
    case 'integer':
    case 'int':
      return addThousands(n.toFixed(0))
    case 'percent':
    case 'percentage':
      return (n * 100).toFixed(2) + '%'
    default:
      return v
  }
}
