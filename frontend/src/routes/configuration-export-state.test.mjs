import { beforeEach, test } from 'bun:test';
import assert from '../test/assert.js';

globalThis.$state = (value) => value;

const {
  configurationExport,
  exportPortableConfiguration,
  portableConfigurationFilename,
  session
} = await import('../lib/admin-state.svelte.js');

const safeFilename = 'n2api-portable-config-v1-20260721T123456Z.json';

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(configurationExport, { exporting: false, notice: null });
});

function downloadDeps(overrides = {}) {
  const calls = { created: [], revoked: [], downloads: [] };
  return {
    calls,
    deps: {
      createObjectURL(blob) {
        calls.created.push(blob);
        return 'blob:portable-configuration';
      },
      revokeObjectURL(url) {
        calls.revoked.push(url);
      },
      triggerDownload(url, filename) {
        calls.downloads.push({ url, filename });
      },
      ...overrides
    }
  };
}

test('portable configuration export downloads the blob with the fixed server filename', async () => {
  const { calls, deps } = downloadDeps({
    fetch: async (path, options) => {
      assert.equal(path, '/api/admin/configuration/export');
      assert.equal(options.headers.Accept, 'application/json');
      return new Response('{"formatVersion":1}', {
        headers: { 'Content-Disposition': `attachment; filename="${safeFilename}"` }
      });
    }
  });

  assert.equal(await exportPortableConfiguration(deps), true);
  assert.equal(await calls.created[0].text(), '{"formatVersion":1}');
  assert.deepEqual(calls.downloads, [{ url: 'blob:portable-configuration', filename: safeFilename }]);
  assert.deepEqual(calls.revoked, ['blob:portable-configuration']);
  assert.equal(configurationExport.exporting, false);
  assert.equal(configurationExport.notice?.kind, 'success');
  assert.equal(configurationExport.notice?.title, 'Download started');
});

test('portable configuration filename parser rejects paths and unexpected names', () => {
  assert.equal(portableConfigurationFilename(`attachment; filename="${safeFilename}"`), safeFilename);
  assert.equal(portableConfigurationFilename(`attachment; filename*=UTF-8''${safeFilename}`), safeFilename);
  for (const disposition of [
    'attachment; filename="../../credentials.json"',
    'attachment; filename="n2api-portable-config-v1-latest.json"',
    'attachment; filename="/tmp/n2api-portable-config-v1-20260721T123456Z.json"',
    null
  ]) {
    assert.equal(portableConfigurationFilename(disposition), 'n2api-portable-config.json');
  }
});

test('portable configuration export reports HTTP failures without starting a download', async () => {
  const { calls, deps } = downloadDeps({
    fetch: async () => Response.json({ error: 'configuration_export_failed' }, { status: 500 })
  });

  assert.equal(await exportPortableConfiguration(deps), false);
  assert.deepEqual(calls.created, []);
  assert.deepEqual(calls.downloads, []);
  assert.equal(configurationExport.exporting, false);
  assert.equal(configurationExport.notice?.kind, 'error');
  assert.equal(configurationExport.notice?.message, 'configuration_export_failed');
});

test('portable configuration export clears the authenticated session after a 401', async () => {
  const { deps } = downloadDeps({
    fetch: async () => Response.json({ error: 'unauthorized' }, { status: 401 })
  });

  assert.equal(await exportPortableConfiguration(deps), false);
  assert.equal(session.authenticated, false);
  assert.equal(session.username, '');
  assert.equal(configurationExport.exporting, false);
  assert.equal(configurationExport.notice, null);
});

test('portable configuration export ignores duplicate requests while busy', async () => {
  let resolveResponse;
  let fetchCalls = 0;
  const { deps } = downloadDeps({
    fetch: async () => {
      fetchCalls += 1;
      return new Promise((resolve) => { resolveResponse = resolve; });
    }
  });

  const first = exportPortableConfiguration(deps);
  await Promise.resolve();
  assert.equal(configurationExport.exporting, true);
  assert.equal(await exportPortableConfiguration(deps), false);
  assert.equal(fetchCalls, 1);

  resolveResponse(new Response('{}', {
    headers: { 'Content-Disposition': `attachment; filename="${safeFilename}"` }
  }));
  assert.equal(await first, true);
  assert.equal(configurationExport.exporting, false);
});

test('portable configuration export revokes the object URL when browser download fails', async () => {
  const { calls, deps } = downloadDeps({
    fetch: async () => new Response('{}'),
    triggerDownload() {
      throw new Error('download unavailable');
    }
  });

  assert.equal(await exportPortableConfiguration(deps), false);
  assert.deepEqual(calls.revoked, ['blob:portable-configuration']);
  assert.equal(configurationExport.notice?.kind, 'error');
  assert.equal(configurationExport.notice?.message, 'download unavailable');
});
