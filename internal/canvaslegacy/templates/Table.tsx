type Column = { key: string; label?: string; type?: string; map?: Record<string,string>; colors?: Record<string,string>; divide?: number; decimals?: number; thousands?: boolean; currency?: string; format?: string; max?: number };
type Data = { [key: string]: unknown; columns?: (string | Column)[]; rows?: unknown[] };
const text = (v: unknown): string => v == null ? '' : typeof v === 'object' ? JSON.stringify(v) : String(v);
function value(row: unknown, key: string, i: number): unknown {
  if (Array.isArray(row)) return row[i];
  if (typeof row !== 'object' || row === null) return row;
  return key.split('.').reduce<unknown>((v,k) => typeof v === 'object' && v !== null ? (v as Record<string,unknown>)[k] : undefined, row);
}
function format(v: unknown, c: Column): string {
  if (v == null || v === '') return '';
  if (c.map || c.type === 'enum') return c.map?.[text(v)] ?? text(v);
  if (['number','currency'].includes(c.type ?? '') || c.currency || c.divide != null || c.decimals != null || c.thousands != null) {
    const n = Number(text(v).replaceAll(',',''));
    if (!Number.isFinite(n)) return text(v);
    const decimals = c.decimals ?? (c.currency || c.type === 'currency' ? 2 : undefined);
    const result = (n / (c.divide || 1)).toLocaleString('en-US', {useGrouping:c.thousands !== false, minimumFractionDigits:decimals, maximumFractionDigits:decimals});
    return (c.currency ?? (c.type === 'currency' ? '¥' : '')) + result;
  }
  if (c.type === 'date' || c.type === 'datetime') {
    const date = new Date(typeof v === 'number' ? (v < 1e12 ? v * 1000 : v) : text(v));
    if (Number.isNaN(date.getTime())) return text(v);
    const parts: Record<string,string> = {YYYY:String(date.getFullYear()), MM:String(date.getMonth()+1).padStart(2,'0'), DD:String(date.getDate()).padStart(2,'0'), HH:String(date.getHours()).padStart(2,'0'), mm:String(date.getMinutes()).padStart(2,'0'), ss:String(date.getSeconds()).padStart(2,'0')};
    return (c.format ?? (c.type === 'date' ? 'YYYY-MM-DD' : 'YYYY-MM-DD HH:mm')).replace(/YYYY|MM|DD|HH|mm|ss/g,k=>parts[k]);
  }
  const s=text(v);return c.type === 'truncate' && s.length > (c.max ?? 30) ? s.slice(0,c.max ?? 30)+'…' : s;
}
export function DataTable({data}:{data:Data}) {
  const rows = data.rows ?? [];
  const columns: Column[] = data.columns?.length ? data.columns.map(c=>typeof c==='string'?{key:c}:c) : Array.isArray(rows[0]) ? rows[0].map((_,i)=>({key:String(i),label:String(i+1)})) : typeof rows[0]==='object' && rows[0]!==null ? Object.keys(rows[0]).map(key=>({key})) : [{key:'value',label:'Value'}];
  const colors:Record<string,string>={red:'#dc2626',amber:'#d97706',green:'#16a34a',sky:'#0284c7',violet:'#7c3aed',gray:'#737373'};
  return <div className="table-scroll"><table><thead><tr>{columns.map((c,i)=><th key={i}>{c.label ?? c.key}</th>)}</tr></thead><tbody>{rows.map((row,i)=><tr key={i}>{columns.map((c,j)=>{const v=value(row,c.key,j);const color=c.colors?.[text(v)];return <td key={j} title={text(v)}><span style={{color: color ? colors[color] ?? color : undefined}}>{format(v,c)}</span></td>})}</tr>)}</tbody></table></div>;
}
