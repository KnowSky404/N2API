import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const buildDir = process.env.N2API_FRONTEND_BUILD_DIR || 'build';

export default defineConfig({
  plugins: [
    tailwindcss(),
    sveltekit({
      preprocess: vitePreprocess(),
      csp: {
        mode: 'hash',
        directives: {
          'default-src': ['self'],
          'base-uri': ['self'],
          'connect-src': ['self'],
          'font-src': ['self'],
          'form-action': ['self'],
          'img-src': ['self', 'data:'],
          'object-src': ['none'],
          'script-src': ['self'],
          'style-src': ['self'],
          'style-src-attr': ['unsafe-inline']
        }
      },
      adapter: adapter({
        pages: buildDir,
        assets: buildDir,
        fallback: '200.html',
        precompress: false,
        strict: true
      })
    })
  ]
});
