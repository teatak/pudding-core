# Turn 文件产物设计

> 状态:第一版已完成(2026-07-16)。

## 1. 目标

每个 Code Turn 结束后记录项目文件变化,在对话中展示紧凑摘要,并允许用户在右侧
画布中查看该 Turn 的多文件 Diff。文件产物属于 Turn,不是工具调用日志,也不依赖
Git 仓库。

第一版支持新增、修改、删除和内容一致的重命名。Chat/Work Turn 不采集。

## 2. 生命周期

```text
首次实际 Code 工具调用前
  -> 快照当前授权 Project roots
  -> 工具正常执行
  -> Turn completed / failed / cancelled
  -> 比较当前文件状态
  -> 文件变化 + canonical message + Turn 状态 + lifecycle event 同事务落库
```

快照必须发生在实际工具调用前,不能依赖模型声明或工具返回结果。Turn 收尾时统一
计算,因此 CLI、patch、Git 和其他 Code 工具产生的文件变化使用同一条路径。

新增的 turn grant 会在它第一次参与 Code 工具调用前补拍基线。权限变化不终止
已经运行的进程,也不改变本 Turn 已经记录的 roots。

## 3. 数据与 API

`turn_file_changes` 保存 `session_id`、`turn_id`、变化类型、旧/新路径、文本内容、
二进制/超限标记以及增删行统计。Turn 列表只返回摘要,不携带文件正文。

详情通过 session-scoped API 按需读取:

```text
GET /sessions/{sessionID}/turns/{turnID}/file-changes/{changeID}
```

服务端同时校验 session、turn 和 change 归属,避免跨会话读取。旧/新正文只在打开
Diff 画布时加载。

## 4. 展示

- 对话在对应 assistant 输出后展示文件清单和 `A/M/D/R` 状态。
- 6 个文件以内全部显示;7 个及以上显示前 5 个和剩余数量。
- 点击任一文件打开右侧画布;同一 Turn 只占一个 Tab。
- 画布顶部可在本 Turn 的文件间切换,文本显示 Diff,二进制和超限文件显示状态。
- 历史 Turn 使用持久化快照,不受工作区后续修改影响。

## 5. 边界

- 单文件文本快照上限 2 MiB,每次快照最多保留 64 MiB 文本正文;二进制和超限文件
  只记录元数据。
- 忽略 `.git`、`node_modules`、`.venv`、`vendor`、构建输出、覆盖率和常见缓存目录。
- 重命名仅在删除项与新增项内容完全一致时识别。
- 第一版按 Turn 前后状态归因。Turn 运行期间用户或其他进程对同一 Project 的改动
  也会进入该 Turn;后台命令在 Turn 结束后才产生的变化不会追记。
- 第一版不提供逐块接受、回滚或直接编辑;这些属于后续变更审阅能力。

## 6. 验收

- 新增、修改、删除、重命名可持久化并在历史 Turn 中恢复。
- failed/cancelled Turn 有文件变化时同样记录。
- Turn 列表不返回文件正文,详情 API 不能跨 session 读取。
- 对话摘要可打开同一 Turn 的多文件 Diff,切换文件不新增 Tab。
- 无 Git 仓库时仍可工作,忽略目录不会产生噪音。
