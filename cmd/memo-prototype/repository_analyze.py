#!/usr/bin/env python3
"""Report factual accuracy separately from citation mechanics and full cost."""
import argparse
import json
from pathlib import Path


def parse_answer(s):
    s = s.strip()
    if s.startswith('```'):
        s = s.split('\n', 1)[-1].removesuffix('```').strip()
    try:
        return json.loads(s)
    except (ValueError, TypeError):
        return {}


def same_json(a, b):
    if isinstance(a, bool) or isinstance(b, bool): return type(a) is type(b) and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)): return a == b
    if type(a) is not type(b): return False
    if isinstance(a, dict): return a.keys() == b.keys() and all(same_json(a[k], b[k]) for k in a)
    if isinstance(a, list): return len(a) == len(b) and all(same_json(x, y) for x, y in zip(a, b))
    return a == b


def analyze(manifest, report, include_pairs=True):
    tasks = {t['id']: t for t in manifest['tasks']}
    groups = {name: dict(runs=0, adoptedTasks=0, releasedTasks=0, attempts=0, invalid=0, acceptedSummaries=0, releasedResults=0,
                        facts=0, factsTotal=0, citations=0, citationsTotal=0, followups=0, mainRequests=0, followupRequests=0,
                        businessCalls=0, readbacks=0, inputCached=0, inputUncached=0, outputContent=0, outputReasoning=0,
                        peakUSD=0., meteringOK=True, unmeteredRequests=0, completedMainTasks=0, errors=[]) for name in ['off', 'on']}
    rows = []
    for r in report.get('runs') or []:
        t = tasks[r['taskID']]
        a = parse_answer(r['main']['answer'])
        if not isinstance(a, dict): a = {}
        answers = a.get('answers', {})
        evidence = a.get('evidence', {})
        if not isinstance(answers, dict): answers = {}
        if not isinstance(evidence, dict): evidence = {}
        wrong = [k for k,v in t['expected'].items() if k not in answers or not same_json(answers[k], v)]
        bad = []
        for k in t['expected']:
            valid = False
            citations = evidence.get(k, [])
            for cite in citations if isinstance(citations, list) else []:
                if not isinstance(cite, dict): continue
                path, start, end = cite.get('file'), cite.get('start_line', 0), cite.get('end_line', 0)
                quote = cite.get('quote', '')
                if not isinstance(path, str) or not isinstance(quote, str): continue
                quote = quote.strip()
                lines = t['files'].get(path, '').split('\n')
                if path in t['evidenceFiles'][k] and isinstance(start, int) and isinstance(end, int) and 1 <= start <= end <= len(lines) and end-start <= 80 and len(quote.encode()) >= 8:
                    valid |= quote in '\n'.join(lines[start-1:end])
            if not valid: bad.append(k)
        row = dict(task=r['taskID'], enabled=r['memo'], facts=len(t['expected'])-len(wrong), citations=len(t['expected'])-len(bad), total=len(t['expected']),
                   answerAvailable=bool(r['main']['answer']), wrongFacts=wrong if r['main']['answer'] else [], unavailableFacts=[] if r['main']['answer'] else list(t['expected']), invalidCitations=bad if r['main']['answer'] else [], followup=r['followup']['correct'], attempts=r['memoAttempts'], applied=r['applied'],
                   requests=[r['main']['requests'],r['followup']['requests']], peakUSD=sum(r[p]['estimatedUSDAtPeakRates'] for p in ['main','followup']))
        rows.append(row)
        g = groups['on' if r['memo'] else 'off']
        g['runs'] += 1; g['adoptedTasks'] += r['memoAttempts'] > r['invalidMemos']; g['releasedTasks'] += r['applied'] > 0
        g['attempts'] += r['memoAttempts']; g['invalid'] += r['invalidMemos']; g['releasedResults'] += r['applied']; g['acceptedSummaries'] += r['acceptedNotes']
        g['facts'] += row['facts']; g['factsTotal'] += row['total']; g['citations'] += row['citations']; g['citationsTotal'] += row['total']; g['followups'] += row['followup']
        g['unmeteredRequests'] += sum(not q['usageSeen'] for q in r['requests']); g['completedMainTasks'] += bool(r['main']['answer']) and not r.get('error');
        g['mainRequests'] += r['main']['requests']; g['followupRequests'] += r['followup']['requests']; g['peakUSD'] += row['peakUSD']; g['meteringOK'] &= r['meteringOK']
        if r.get('error'): g['errors'].append({'task':r['taskID'],'error':r['error']})
        for phase in ['main','followup']:
            p = r[phase]; g['businessCalls'] += p['businessCalls']; g['readbacks'] += p['readbackCalls']
            for dst, src in [('inputCached','InputCachedTokens'),('inputUncached','InputUncachedTokens'),('outputContent','OutputContentTokens'),('outputReasoning','OutputReasoningTokens')]: g[dst] += p['usage'][src]
        assert row['facts'] + row['citations'] == r['main']['checksPassed'], 'grader disagreement'
    for g in groups.values():
        g['costIsLowerBound'] = g['unmeteredRequests'] > 0
        g['answeredFactsTotal'] = sum(row['total'] for row in rows if row['enabled'] == (g is groups['on']) and row['answerAvailable'])
    result = {'groups':groups,'rows':rows}
    if include_pairs:
        paired = {}
        for r in report.get('runs') or []: paired.setdefault(r['taskID'], {})[r['memo']] = r
        complete = [k for k,v in paired.items() if len(v)==2 and all(r['meteringOK'] and not r.get('error') for r in v.values())]
        retained = [r for r in report.get('runs') or [] if r['taskID'] in complete]
        pair_groups = analyze(manifest, {'runs':retained}, False)['groups']
        result['completePairComparison'] = {'taskIDs':complete,'groups':pair_groups,
            'costChangePercent':(pair_groups['on']['peakUSD']/pair_groups['off']['peakUSD']-1)*100 if pair_groups['off']['peakUSD'] else None}
    return result


def main():
    p=argparse.ArgumentParser();p.add_argument('manifest',type=Path);p.add_argument('report',type=Path);p.add_argument('--output',type=Path)
    a=p.parse_args();data=analyze(json.loads(a.manifest.read_text()),json.loads(a.report.read_text()));out=json.dumps(data,ensure_ascii=False,indent=2)+'\n'
    if a.output:
        with a.output.open('x') as f:f.write(out)
    print(out)

if __name__ == '__main__': main()
