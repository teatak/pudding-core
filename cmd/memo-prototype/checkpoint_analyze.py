#!/usr/bin/env python3
"""Analyze every planned old/new checkpoint run without selecting successes."""
import argparse
import hashlib
import json
from pathlib import Path

from brief_analyze import summarize


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('--date', default='2026-10-19')
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    manifest_path = args.directory / f'summarize-results-repository-fixtures-{args.date}.json'
    metadata_path = args.directory / f'summarize-results-checkpoint-run-metadata-{args.date}.json'
    manifest = json.loads(manifest_path.read_text())
    metadata = json.loads(metadata_path.read_text())
    assert metadata['status'] in ('runs-completed', 'complete')
    assert metadata['originalManifestSHA256'] == digest(manifest_path)
    assert len(metadata['outcomes']) == len(metadata['plannedRuns']) == 8
    task_by_id = {t['id']: t for t in manifest['tasks']}
    reports = {arm: [] for arm in ('base', 'candidate')}
    sources = {str(manifest_path): digest(manifest_path)}
    for plan, outcome in zip(metadata['plannedRuns'], metadata['outcomes']):
        assert all(plan[k] == outcome[k] for k in plan)
        source = root / plan['manifest']
        frozen = json.loads(source.read_text())
        assert frozen['tasks'] == [task_by_id[plan['taskID']]]
        assert digest(source) == plan['manifestSHA256']
        path = root / plan['output']
        report = json.loads(path.read_text())
        assert digest(path) == outcome['reportSHA256']
        assert report['manifestSHA256'] == digest(source)
        assert report['harnessVersion'] == '5' and report['arm'] == 'memo'
        assert report['requestedModel'] == 'deepseek-flash'
        assert len(report['runs']) == report['plannedRuns'] == 1
        assert report['runs'][0]['taskID'] == plan['taskID']
        reports[plan['arm']].append(report)
        sources[plan['output']] = digest(path)
    groups = {}
    for arm, values in reports.items():
        assert len({r['instructions'] for r in values}) == 1
        merged = {k: values[0][k] for k in ('schemaVersion', 'generatedAt', 'harnessVersion')}
        merged['runs'] = [r['runs'][0] for r in values]
        summary = summarize(manifest, merged)
        for row, run in zip(summary['rows'], merged['runs']):
            candidate_refs = set()
            candidate_opportunities = []
            for index, request in enumerate(run['requests'], 1):
                candidates = [v for v in request.get('resultViews', []) if v.get('summaryCandidate')]
                assert all(v['view'] == 'content' and v['contentBytes'] >= 8192 for v in candidates)
                candidate_refs.update((v['ref']['turn_id'], v['ref']['call_id']) for v in candidates)
                if candidates and any(o['request'] == index for o in row['opportunities']):
                    candidate_opportunities.append(index)
            row['uniqueCandidateResultsSeen'] = len(candidate_refs)
            row['candidateOpportunityRequests'] = candidate_opportunities
            row['firstInputTokens'] = sum(run['requests'][0]['usage'][k] for k in ('InputCachedTokens', 'InputUncachedTokens'))
            row['allResponsesComplete'] = all(q['complete'] for q in run['requests'])
        summary['groups']['candidateOpportunityRequests'] = sum(len(r['candidateOpportunityRequests']) for r in summary['rows'])
        summary['groups']['uniqueCandidateResultsSeen'] = sum(r['uniqueCandidateResultsSeen'] for r in summary['rows'])
        groups[arm] = summary
    pairs = []
    for old, new in zip(groups['base']['rows'], groups['candidate']['rows']):
        assert old['task'] == new['task']
        pairs.append({'task': old['task'], 'attempts': [old['attempts'], new['attempts']],
                      'releases': [old['applied'], new['applied']],
                      'requests': [sum(old['requests']), sum(new['requests'])],
                      'businessCalls': [old['businessCalls'], new['businessCalls']],
                      'readbacks': [old['readbacks'], new['readbacks']],
                      'facts': [old['facts'], new['facts']], 'citations': [old['citations'], new['citations']],
                      'followup': [old['followup'], new['followup']],
                      'costUSD': [old['peakUSD'], new['peakUSD']],
                      'firstInputDelta': new['firstInputTokens'] - old['firstInputTokens']})
    result = {'version': 'checkpoint-paired-analysis-v1', 'sourceSHA256': sources,
              'arms': groups, 'pairs': pairs,
              'caveats': ['Each task/arm ran once, in counterbalanced order; no statistical or isolated-variable causality claim.',
                          'The candidate changes both conditional instructions and a stable size cue; effects cannot be separated.',
                          'Summary length is Unicode code points; no emitted summary means no length sample.',
                          'Opportunity is structural availability, not proof that required evidence can be discarded.',
                          'All main/follow-up usage is included at historical uniform peak rates; cache is observed, not controlled.',
                          'Failures are retained and never retried. No dedicated cleanup/correction model request was added.']}
    with args.output.open('x') as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
        f.write('\n')
    for arm in groups:
        print(arm, json.dumps(groups[arm]['groups'], ensure_ascii=False))
    print('pairs', json.dumps(pairs, ensure_ascii=False))


if __name__ == '__main__':
    main()
