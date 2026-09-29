import { BarChart, Bar, LineChart, Line, AreaChart, Area, PieChart, Pie, Cell, ResponsiveContainer, XAxis, YAxis, Tooltip, Legend } from 'recharts';
type Series={key:string;label?:string;color?:string};
type Spec={[key:string]:unknown;type?:string;data?:Record<string,unknown>[];x_key?:string;xKey?:string;name_key?:string;value_key?:string;valueKey?:string;series?:(Series|string)[]};
export function Chart({spec}:{spec:Spec}) {
 const colors=['#6366f1','#14b8a6','#f59e0b','#ec4899','#38bdf8'];
 const data=(spec.data ?? []).map(row=>Object.fromEntries(Object.entries(row).map(([k,v])=>[k,typeof v==='string' && v.trim()!=='' && Number.isFinite(Number(v.replaceAll(',',''))) ? Number(v.replaceAll(',','')) : v])));
 const first=data[0] ?? {};const x=spec.x_key ?? spec.xKey ?? Object.keys(first).find(k=>typeof first[k]!=='number') ?? Object.keys(first)[0] ?? 'name';
 const series:Series[]=spec.series?.length ? spec.series.map(s=>typeof s==='string'?{key:s}:s) : spec.value_key || spec.valueKey ? [{key:spec.value_key ?? spec.valueKey!}] : Object.keys(first).filter(k=>k!==x&&typeof first[k]==='number').map(key=>({key}));
 const circular=spec.type==='pie'||spec.type==='donut';
 const shape=spec.type==='line'?'line':spec.type==='area'?'area':'bar';
 const Cartesian=shape==='line'?LineChart:shape==='area'?AreaChart:BarChart;
 return <div style={{width:'100%',minWidth:0,height:300}}><ResponsiveContainer width="100%" height="100%">{circular ? <PieChart><Tooltip/><Legend/><Pie isAnimationActive={false} data={data} nameKey={spec.name_key ?? spec.x_key ?? 'name'} dataKey={spec.value_key ?? spec.valueKey ?? series[0]?.key ?? 'value'} innerRadius={spec.type==='donut'?'45%':0} outerRadius="75%">{data.map((_,i)=><Cell key={i} fill={colors[i%colors.length]}/>)}</Pie></PieChart> : <Cartesian data={data}><XAxis dataKey={x}/><YAxis/><Tooltip/><Legend/>{series.map((s,i)=>{const color=s.color ?? colors[i%colors.length];return shape==='line'?<Line isAnimationActive={false} key={s.key} dataKey={s.key} name={s.label ?? s.key} stroke={color} dot={false}/>:shape==='area'?<Area isAnimationActive={false} key={s.key} dataKey={s.key} name={s.label ?? s.key} stroke={color} fill={color} fillOpacity={.2}/>:<Bar isAnimationActive={false} key={s.key} dataKey={s.key} name={s.label ?? s.key} fill={color}/>})}</Cartesian>}</ResponsiveContainer></div>;
}
