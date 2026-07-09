// 路由 search 参数形状,与 main.tsx 的 validateSearch 保持同步。
// TanStack Router 的函数式 search 更新器把 prev 推导成含父路由 `{}` 的
// 联合类型,各更新器先用此类型收窄再操作。
export type AppSearch = {
  session?: string;
  draft?: string;
  project?: string;
  split?: string;
  view?: "apps";
};
