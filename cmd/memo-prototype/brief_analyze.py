#!/usr/bin/env python3
"""Compare the brief-prompt rerun with historical enabled runs, not off/on."""
import argparse
import hashlib
import json
from collections import Counter
from pathlib import Path
from statistics import mean, median

from repository_analyze import analyze


def summarize(manifest, report, prior_audit=None):
    enabled = [r for r in report['runs'] if r['memo']]
    base = analyze(manifest, {'runs': enabled}, include_pairs=False)
    rows = []
    lengths = []
    for run, graded in zip(enabled, base['rows']):
        opportunities, controls, signatures = [], [], []
        traced = report.get('harnessVersion') in ('4', '5')
        for index, request in enumerate(run['requests'], 1):
            visible = [v for v in request.get('resultViews', []) if v['view'] == 'content']
            calls = request.get('calls', [])
            eligible = [c for c in calls if c['name'] in ('builtin_file_slice', 'builtin_history_get_message')]
            if request['memoTools'] and visible and eligible:
                opportunities.append({'request': index, 'phase': request['phase'],
                                      'visibleResults': len(visible), 'eligibleCalls': len(eligible)})
            for call in calls:
                args = json.loads(call['arguments'])
                if 'summarize_results' in args:
                    control = args.pop('summarize_results')
                    summary = control.get('summary') if isinstance(control, dict) else None
                    n = len(summary) if isinstance(summary, str) else None
                    if n is not None:
                        lengths.append(n)
                    controls.append({'request': index, 'phase': request['phase'], 'callID': call['id'],
                                     'tool': call['name'], 'summaryChars': n,
                                     'exceeds800': n > 800 if n is not None else None,
                                     'control': control})
                signatures.append((request['phase'], call['name'], json.dumps(args, sort_keys=True)))
        if traced:
            assert len(controls) == run['memoAttempts'], 'wire/canonical attempt mismatch'
            opportunity_count = len(opportunities)
        else:
            # The first harness had no per-call trace. Counts were previously
            # corroborated from its retained log; never infer missing traces as zero.
            audit = next(r for r in prior_audit['rows'] if r['task'] == run['taskID'])
            assert audit['controlAttempts'] == run['memoAttempts'] == 0
            opportunity_count = audit['structuralNextReadOpportunities']
        rows.append({**graded, 'invalidControls': run['invalidMemos'],
                     'validControls': run['memoAttempts'] - run['invalidMemos'],
                     'acceptedSummaries': run['acceptedNotes'], 'skipped': run['skipped'],
                     'releasedSent': max(q['releasedResults'] for q in run['requests']),
                     'structuralOpportunityRequests': opportunity_count,
                     'opportunities': opportunities if traced else None,
                     'controls': controls,
                     'businessCalls': sum(run[p]['businessCalls'] for p in ('main', 'followup')),
                     'readbacks': sum(run[p]['readbackCalls'] for p in ('main', 'followup')),
                     'repeatedIdenticalCalls': sum(n - 1 for n in Counter(signatures).values() if n > 1) if traced else None,
                     'instructionsSeenEveryRequest': all(q['instructionsSeen'] for q in run['requests']) if traced else None,
                     'memoToolsPerRequest': [q['memoTools'] for q in run['requests']],
                     'meteringOK': run['meteringOK'], 'error': run.get('error')})
    group = base['groups']['on']
    group.update(tasksWithAttempts=sum(r['attempts'] > 0 for r in rows),
                 structuralOpportunityRequests=sum(r['structuralOpportunityRequests'] for r in rows),
                 summaryLengths={'samples': len(lengths), 'values': lengths,
                                 'min': min(lengths) if lengths else None,
                                 'max': max(lengths) if lengths else None,
                                 'mean': mean(lengths) if lengths else None,
                                 'median': median(lengths) if lengths else None,
                                 'exceeds800': sum(n > 800 for n in lengths)})
    return {'schemaVersion': report['schemaVersion'], 'generatedAt': report['generatedAt'],
            'harnessVersion': report.get('harnessVersion'), 'groups': group, 'rows': rows}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('--date', default='2026-10-19')
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    stem = 'summarize-results-'
    names = {'manifest': f'{stem}repository-fixtures-{args.date}.json',
             'previous': f'{stem}repository-deepseek-{args.date}.json',
             'current': f'{stem}brief-deepseek-{args.date}.json',
             'audit': f'{stem}trigger-prior-audit-{args.date}.json'}
    raw = {k: (args.directory / v).read_bytes() for k, v in names.items()}
    data = {k: json.loads(v) for k, v in raw.items()}
    manifest_sha = hashlib.sha256(raw['manifest']).hexdigest()
    assert data['previous']['manifestSHA256'] == data['current']['manifestSHA256'] == manifest_sha
    assert len(data['current']['runs']) == data['current']['plannedRuns'] == len(data['manifest']['tasks'])
    assert data['previous']['requestedModel'] == data['current']['requestedModel'] == 'deepseek-flash'
    old = summarize(data['manifest'], data['previous'], data['audit'])
    new = summarize(data['manifest'], data['current'])
    assert [r['task'] for r in old['rows']] == [r['task'] for r in new['rows']]
    result = {'version': 'brief-historical-comparison-v1',
              'sourceSHA256': {names[k]: hashlib.sha256(v).hexdigest() for k, v in raw.items()},
              'previous': old, 'current': new,
              'metricDefinitions': {
                  'attempt': 'Presence of summarize_results on a normal tool call, even when invalid.',
                  'validControl': 'Attempt accepted by local structural/length parsing; not proof of actual release.',
                  'structuralOpportunityRequests': 'Already-visible content results plus a subsequent eligible normal call; does not prove semantic discardability.',
                  'summaryChars': 'Unicode code points, matching the local Go rune limit. No emitted summary gives null length statistics, not zero.',
                  'cost': 'All main/follow-up requests, cached/uncached input, content/reasoning output; historical uniform peak estimates, not an invoice.'},
              'caveats': ['One run per task, historical rather than randomized contemporaneous comparison.',
                          'Same frozen task prompts and source bytes; cache, batching and model randomness can differ.',
                          'HTTP/phase timeouts increased from 120/300s to 240/600s; old enabled runs all completed.',
                          'The old 1143-character attempt belongs to a separate result-hint probe, not this four-task baseline.',
                          'No dedicated cleanup or correction requests; no failed outcomes dropped or retried.']}
    with args.output.open('x') as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
        f.write('\n')
    for name in ('previous', 'current'):
        print(name, json.dumps(result[name]['groups'], ensure_ascii=False))


if __name__ == '__main__':
    main()
