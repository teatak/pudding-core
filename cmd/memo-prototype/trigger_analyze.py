#!/usr/bin/env python3
"""Analyze the frozen 2x2 prompt/workflow diagnosis, without reading thoughts."""
import argparse
import json
from collections import Counter
from pathlib import Path

from repository_analyze import analyze


def diagnostic(manifest, report):
    base = analyze(manifest, report, include_pairs=False)
    rows = []
    for run, row in zip(report['runs'], base['rows']):
        opportunities, controls, business = [], [], []
        for index, request in enumerate(run['requests'], 1):
            visible = [v for v in request.get('resultViews', []) if v['view'] == 'content']
            calls = request.get('calls', [])
            # This is structural opportunity, not proof that details are safe
            # to discard. Semantic sufficiency remains the model's decision.
            if visible and calls and request['memoTools']:
                opportunities.append({'request': index, 'phase': request['phase'],
                    'visibleResults': len(visible), 'contentBytes': sum(v['contentBytes'] for v in visible)})
            for call in calls:
                try:
                    args = json.loads(call['arguments'])
                except ValueError:
                    continue
                control = args.pop('summarize_results', None)
                business.append((request['phase'], call['name'], json.dumps(args, sort_keys=True)))
                if control is not None:
                    summary = control.get('summary') if isinstance(control, dict) else None
                    controls.append({'request': index, 'phase': request['phase'], 'carrier': call,
                        'summaryChars': len(summary) if isinstance(summary, str) else None,
                        'exceedsSummaryLimit': isinstance(summary, str) and len(summary) > 800})
        row.update(schemaVersion=report['schemaVersion'], opportunities=opportunities, controls=controls,
            repeatedIdenticalCalls=sum(n-1 for n in Counter(business).values() if n > 1),
            businessCalls=run['main']['businessCalls']+run['followup']['businessCalls'],
            readbackCalls=run['main']['readbackCalls']+run['followup']['readbackCalls'],
            acceptedSummaries=run['acceptedNotes'], invalidControls=run['invalidMemos'], skipped=run['skipped'],
            releasedSent=max((q['releasedResults'] for q in run['requests']), default=0),
            instructionPresentEveryRequest=all(q['instructionsSeen'] for q in run['requests']),
            meteringOK=run['meteringOK'], error=run.get('error'),
            usage=base['groups']['on'],
            caveat='One run per cell. Timing variant also changes the user workflow. No statistical adoption or savings claim.')
        rows.append(row)
    return rows


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('--date', default='2026-10-19')
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--probes', action='store_true', help='also include the subsequent v3/v4 exploratory probes')
    args = parser.parse_args()
    rows = []
    arms = [('v1', 'original'), ('v2', 'original'), ('v2', 'staged'), ('v1', 'staged')]
    if args.probes:
        arms += [('v3-probe', 'original'), ('v4-probe', 'original')]
    for version, variant in arms:
        stem = f'summarize-results-trigger-'
        manifest = json.loads((args.directory / f'{stem}{variant}-fixture-{args.date}.json').read_text())
        report = json.loads((args.directory / f'{stem}{version}-{variant}-{args.date}.json').read_text())
        for row in diagnostic(manifest, report):
            row['arm'] = f'{version}-{variant}'
            rows.append(row)
    with args.output.open('x') as f:
        json.dump({'version': 'trigger-analysis-v1', 'rows': rows}, f, ensure_ascii=False, indent=2)
        f.write('\n')
    for row in rows:
        print(row['arm'], 'opportunities=', len(row['opportunities']), 'controls=', row['attempts'],
              'released=', row['applied'], 'facts/citations=', (row['facts'], row['citations']),
              'requests=', row['requests'], 'USD=', row['peakUSD'])


if __name__ == '__main__':
    main()
