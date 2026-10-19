#!/usr/bin/env python3
"""Freeze allowlisted real source and a host-only rubric before any model run."""
import argparse
import hashlib
import json
from pathlib import Path


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--core', type=Path, required=True)
    ap.add_argument('--desktop', type=Path, required=True)
    ap.add_argument('--output', type=Path, required=True)
    args = ap.parse_args()
    roots = {'core': args.core, 'desktop': args.desktop}
    tasks = []

    def task(task_id, goal, paths, questions, followup, followup_expected):
        files = {}
        for path in paths:
            repo, relative = path.split('/', 1)
            files[path] = (roots[repo] / relative).read_text()
        prompt = goal + '\n这是当前工作区源码的冻结只读副本。请结合实现与回归测试，逐项追踪实际行为，不运行测试、不修改文件，不把测试夹具中的声明当成实现保证。可用工具仅为 builtin_file_slice（scope=project）和 builtin_history_get_message。\n可读源码（括号为总行数）：\n'
        prompt += '\n'.join(f'- {p} ({len(s.splitlines())} lines)' for p, s in files.items())
        prompt += '\n需要核对的问题（answers 中使用下列键和指定的 JSON 值类型）：\n'
        prompt += '\n'.join(f'- {key}: {q}' for key, q, expected, evidence in questions)
        prompt += '\n最后只返回 JSON 对象：{"answers": {"问题键": 值}, "evidence": {"问题键": [{"file":"上述相对路径","start_line":整数,"end_line":整数,"quote":"该行范围内逐字复制的一小段源码，不含行号前缀"}]}, "explanation":"简要解释跨文件关系及风险边界"}。每个答案都要有具体源码引用，不能只给整个文件；找不到证据时明确说明，不猜测。'
        tasks.append({'id': task_id, 'prompt': prompt, 'followup': followup + '\n只返回所要求的 JSON 值。使用此前审查的原始证据；副本现已变化，如需补证据请回读原工具结果，不读取当前文件。',
                      'files': files, 'sha256': {p: hashlib.sha256(s.encode()).hexdigest() for p,s in files.items()},
                      'expected': {k:e for k,q,e,f in questions}, 'evidenceFiles': {k:f for k,q,e,f in questions}, 'followupExpected': followup_expected})

    dp = 'desktop/web/src/state/draftPersistence.ts'
    ds = 'desktop/web/src/state/draftStore.ts'
    ss = 'desktop/web/src/state/sessionDraftStore.ts'
    au = 'desktop/web/src/lib/attachmentUploads.ts'
    dt = 'desktop/web/test/draft-persistence.test.ts'
    st = 'desktop/web/test/session-draft-submission.test.ts'
    at = 'desktop/web/test/attachment-uploads.test.ts'
    da = 'desktop/web/test/draft-acknowledgement.test.ts'
    delete = 'desktop/web/src/lib/deleteSessionAndDrafts.ts'
    task('draft-restart', '审查桌面草稿从输入、发送快照到刷新恢复、删除的完整链路，解释是否会丢输入或重复提交。',
         [dp, ds, ss, au, delete, dt, st], [
         ('storage', '草稿内容持久化使用哪个浏览器存储 API？返回 API 名称字符串。', 'localStorage', [dp]),
         ('debounce_ms', '普通非空文本修改的持久化防抖是多少毫秒？整数。', 250, [dp]),
         ('unfinished_upload_restored', '上传未完成的 File 本体是否会恢复？布尔值。', False, [dp, dt]),
         ('empty_queue_edit_restored', '有意清空但尚未提交的队列编辑是否能独立于普通草稿恢复？布尔值。', True, [dp, dt]),
         ('retry_same_message_id', '已发送快照未改变，刷新后重试是否复用 clientMessageID？布尔值。', True, [ss, dp, dt]),
         ('late_ack_keeps_new_text', '旧发送确认回来时，之后新输入的不同文本是否保留？布尔值。', True, [ss, st]),
         ('bad_entry_blocks_all', '一个损坏的持久化条目是否阻止其他草稿加载？布尔值。', False, [dp, dt]),
         ('delete_failure_keeps_draft', '服务端删除会话失败时，本地草稿是否保留？布尔值。', True, [delete, dt]),
         ('new_conversation_retry_id', '新会话同项目、相同输入重试是否复用 clientRequestID？布尔值。', True, [ds])],
         '离开页面时触发强制 flush 的 window 事件名称是什么？返回 JSON 字符串。', 'pagehide')

    task('attachment-acknowledgement', '审查附件上传、重试、提交排序和延迟确认之间的竞态；重点判断用户边发边编辑时哪些内容会被保留。',
         [au, ds, ss, 'desktop/web/src/lib/submitParts.ts', at, da, st, 'desktop/web/test/queued-draft-acknowledgement.test.ts'], [
         ('empty_file_uploaded', 'addFiles 会不会上传 size=0 的文件？布尔值。', False, [au]),
         ('failed_payload_retained', '上传失败时是否保留 File 与上传目标用于重试？布尔值。', True, [au, at]),
         ('success_payload_retained', '上传成功的条目是否仍携带原 File 和 upload 目标？布尔值。', False, [au]),
         ('late_upload_revives_removed', '完成较晚的上传是否会让已删除条目重新出现？布尔值。', False, [au, at]),
         ('double_retry_requests', '同一 error 条目被立即连续重试两次，最多启动几次新的上传请求？整数。', 1, [au, at]),
         ('replacement_kept_after_ack', '发送后用同一 ID 的新对象替换附件，旧确认到达时新对象是否保留？布尔值。', True, [ds, ss, da, st]),
         ('changed_input_reuses_message_id', '已准备会话发送后实际改动内容再发送，是否复用旧 clientMessageID？布尔值。', False, [ss, st]),
         ('queue_key_is_session_id', 'queuedDraftKey 返回值是否允许作为后端业务 sessionID 使用？布尔值。', False, [ss]),
         ('order_deduplicated', 'orderedDraftItems 是否按类型去重同 ID 并保留可用卡片顺序？布尔值。', True, ['desktop/web/src/lib/submitParts.ts'])],
         '上传目标是 draft 且服务端返回 404 时，条目的 errorKey 精确值是什么？返回 JSON 字符串。', 'composer.draftUploadUnavailable')

    wi='core/internal/api/widget_install.go'; wd='core/internal/widget/distribution.go'; wt='core/internal/api/widget_install_test.go'; wo='core/internal/store/sqlitestore/widget_origins.go'; ws='core/internal/config/widget_sources.go'
    task('widget-install-upgrade', '对 Widget 分发包校验、安装幂等、升级及本地修改保护做端到端审查，区分源码 head、运行版本和持久业务数据。',
         [wi,wd,wo,ws,wt,'core/internal/widget/distribution_test.go','core/internal/widget/package.go','core/internal/widget/draft.go'], [
         ('http_registry_accepted','registryURL=http://example.com/registry.json 是否接受？布尔值。',False,[ws]),
         ('reinstall_duplicates','相同 registryURL/packageID 使用新 clientRequestID 重装，是否创建第二个原始安装？布尔值。',False,[wi,wo,wt]),
         ('upgrade_activates','升级到不同源码是否立即切换 ActiveRevision？布尔值。',False,[wo,wt]),
         ('upgrade_keeps_data','升级原始安装是否保留已有 widget_data？布尔值。',True,[wt,wo]),
         ('copy_upgrade_allowed','Origin.Copy=true 的自定义副本是否允许走包升级？布尔值。',False,[wi,wo,wt]),
         ('edited_draft_error','升级时 working draft 哈希与当前 head 不同，接口 error 字符串是什么？', 'widget_local_changes',[wi]),
         ('same_version_changed_hash_allowed','存储层是否允许同一版本号但不同 PackageHash 的升级？布尔值。',False,[wo]),
         ('changed_file_hash_accepted','外层包哈希正确但某个文件内容与 FileHashes 不一致，DecodeDistribution 是否接受？布尔值。',False,[wd,'core/internal/widget/distribution_test.go']),
         ('archived_reinstall_conflicts','同一包的原始安装已归档，再安装是否冲突？布尔值。',True,[wi,wo,wt])],
         '安装接口对 clientRequestID 的长度上下界是多少？返回 {"min":整数,"max":整数}。', {'min':8,'max':100})

    hr='core/internal/tool/history_result.go'; cr='core/internal/tool/context_result.go'; ht='core/internal/tool/history_result_test.go'; ct='core/internal/tool/context_result_test.go'
    task('history-result-readback', '审查工具大结果预览与历史精确回读的边界，判断模型怎样拿回原证据，以及分页、过滤和不可信结果如何处理。',
         [hr,cr,'core/internal/tool/history.go',ht,ct,'core/internal/tool/history_test.go'], [
         ('default_char_limit','未指定 unit/limit 的结果回读默认多少字符？整数。',4000,[cr,hr]),
         ('maximum_char_limit','chars 分页允许的最大 limit 是多少？整数。',8000,[cr,hr]),
         ('duplicate_call_error','同一 turn 有两个相同 call_id 的结果时，错误 reason 是什么？字符串。','ambiguous_result',[hr,ht]),
         ('query_case_sensitive','lines 的 query 匹配是否区分大小写？布尔值。',True,[hr,ht]),
         ('filtered_offsets_renumbered','带 query 时 offset/next_offset 是否改成过滤后数组的位置？布尔值。',False,[hr,ht]),
         ('json_string_as_items','unit=items 是否接受内容是 JSON 数组字符串而非真实数组的字段？布尔值。',False,[hr,ht]),
         ('shorten_without_reader','不具备回读工具时是否会按 12000 字符阈值截断普通大结果？布尔值。',False,[cr,ct]),
         ('slice_duplicates_content','file_slice 有合法 numberedContent 时模型视图是否仍同时保留重复 content 字段？布尔值。',False,[cr,ct]),
         ('readback_reexecutes','historyReadResult 是否通过重新执行原工具来读取结果？布尔值。',False,[hr,ht])],
         '历史回读中 lines 和 items 的默认分页条数各是多少？返回 {"lines":整数,"items":整数}。', {'lines':40,'items':20})

    output = {'version':'repository-review-v1','description':'Real working-tree source; frozen before paired runs. Host-only expected values and rubrics never enter model project.', 'tasks':tasks}
    with args.output.open('x') as f:
        json.dump(output, f, ensure_ascii=False, indent=2)
        f.write('\n')
    print(json.dumps([{'id':t['id'],'files':len(t['files']),'lines':sum(len(x.splitlines()) for x in t['files'].values()),'bytes':sum(len(x.encode()) for x in t['files'].values()),'facts':len(t['expected'])} for t in tasks]))

if __name__ == '__main__':
    main()
