#!/usr/bin/env python3
"""Exercise recovery/search fixes through a compiled stdio MCP in a disposable library."""
import argparse
from pathlib import Path
import tempfile

from smoke import Host, fixture_profile, mode_args


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    opt = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix='memory review recovery ') as temp:
        host = Host(str(Path(opt.binary).resolve()), temp, mode="strict")
        try:
            boot = host.tool('memory_bootstrap', {})
            if 'review_recovery' not in boot.get('features', []):
                assert opt.if_supported
                print('SKIP: historical release predates review recovery')
                return
            scope = 'project:review-recovery-fixture'
            def raw(body, links=None):
                return host.tool('memory_record', dict(kind='record', scope=scope, title='Actual fixture wording',
                                 body=body, origin='user_supplied', links=links or []))
            def draft(source, **extra):
                result = dict(kind='case', scope=scope, title='Fixture conclusion', body='Decision with explicit fixture boundaries',
                              origin='model_inference', profile=fixture_profile(), links=[dict(target_id=source['id'], relation='supports')])
                result.update(extra)
                return result
            def read(entry):
                return host.tool('memory_read', {'id': entry['id']})['entry']
            def change(entry, wording):
                return host.tool('memory_revise', dict(previous_id=entry['id'], draft=dict(kind='record', scope=scope,
                                 title='Corrected actual fixture wording', body=wording, origin='user_supplied')))
            def op(entry, *sources):
                return dict(entry_id=entry['id'], action='recheck', reason='Read current evidence and verified unchanged fixture applicability',
                            evidence_ids=[e['id'] for e in sources])
            def apply(plan):
                yes = raw('Apply this exact displayed fixture recheck plan.')
                return host.tool('memory_cleanup', dict(action='apply', scope=scope, plan_id=plan['id'],
                                 approval_source_id=yes['id'], approval_quote=yes['body']))
            original = raw('Initial fixture evidence')
            upstream = host.tool('memory_record', draft(original))
            downstream = host.tool('memory_record', draft(upstream))
            current = change(original, 'Corrected fixture evidence, conclusion remains scoped to this fixture')
            assert read(upstream)['needs_review'] and read(downstream)['needs_review']
            for relation in ('context', 'contradicts'):
                correction = raw('原文更正：待复查不应阻止记录这个事实。', [dict(target_id=upstream['id'], relation=relation)])
                assert not correction['needs_review'] and read(upstream)['needs_review']
            host.tool('memory_record', draft(upstream), expect_error=True)
            host.tool('memory_record', draft(upstream, links=[dict(target_id=upstream['id'], relation='context')]), expect_error=True)
            error = host.tool('memory_cleanup', dict(action='recheck_sources', scope=scope, entry_id=downstream['id'],
                              source_ids=[upstream['id']], note='Cannot bless a still-pending upstream'), expect_error=True)
            message = error['content'][0]['text']
            assert upstream['id'] in message and downstream['id'] in message
            plan = host.tool('memory_cleanup', dict(action='preview', scope=scope,
                             operations=[op(upstream, current), op(downstream, upstream)]))['plan']
            assert read(upstream)['needs_review'] and read(downstream)['needs_review']
            apply(plan)
            assert not read(upstream)['needs_review'] and not read(downstream)['needs_review']
            current = change(current, 'Another correction to actual fixture evidence')
            assert read(upstream)['needs_review'] and read(downstream)['needs_review']
            corrected = draft(current, links=[dict(target_id=current['id'], relation='supports'),
                                             dict(target_id=upstream['id'], relation='context')])
            revised = host.tool('memory_revise', dict(previous_id=upstream['id'], draft=corrected))
            assert not revised['needs_review'] and read(upstream)['body'] == upstream['body']

            fresh = raw('Fresh evidence for a separate fixture decision')
            weak = host.tool('memory_record', draft(fresh, links=[dict(target_id=fresh['id'], relation='supports'),
                                               dict(target_id=downstream['id'], relation='contradicts')]))
            fresh = change(fresh, 'Fresh wording updated, conclusion still applies')
            plan = host.tool('memory_cleanup', dict(action='recheck_sources', scope=scope, entry_id=weak['id'],
                             source_ids=[fresh['id'], downstream['id']], note='Reassessed evidence and read unresolved opposing target'))['plan']
            apply(plan)
            assert not read(weak)['needs_review'] and read(downstream)['needs_review']

            needle = raw('only_last_term_12')
            source = raw('结合当前项目检查回填字段及需求入口。')
            keywords = ['  ', 'DUPLICATE', 'duplicate'] + [f'only_last_term_{i:02d}' for i in range(1, 13)]
            result = host.tool('memory_context', dict(action='prepare', scope=scope, source_id=source['id'],
                               task='结合项目和前文检查当前需求，而非只按用户最后一句搜索', keywords=keywords))
            assert result['keyword_search']['status'] == 'searched' and len(result['keyword_search']['used_terms']) == 13
            assert needle['id'] in [hit['id'] for hit in result['matches']]
            result = host.tool('memory_context', dict(action='prepare', scope=scope, source_id=source['id'],
                               task='Browse the directory when the query exceeds the work budget', keywords=['线索' * 1100]))
            assert result['keyword_search']['status'] == 'directory_only' and result['directory']
        finally:
            host.close()
    print('PASS: stdio correction references, anti-laundering, direct flagged-target revision, preview blockers, ordered approved recovery, future invalidation, all search terms and explicit directory fallback.')


if __name__ == '__main__':
    main()
