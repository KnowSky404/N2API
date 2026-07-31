import DOMPurify from 'dompurify';
import { Marked } from 'marked';

const markdown = new Marked({
  gfm: true,
  breaks: false,
  renderer: {
    html() {
      return '';
    }
  }
});

const allowedTags = [
  'a', 'blockquote', 'br', 'code', 'del', 'em', 'h1', 'h2', 'h3', 'h4', 'hr',
  'li', 'ol', 'p', 'pre', 'strong', 'table', 'tbody', 'td', 'th', 'thead', 'tr', 'ul'
];

/** @param {unknown} notes */
export function renderReleaseNotes(notes) {
  if (typeof document === 'undefined') return '';
  const source = String(notes ?? '').replace(/^\uFEFF/, '');
  const rendered = markdown.parse(source, { async: false });
  if (typeof rendered !== 'string') return '';
  const sanitized = DOMPurify.sanitize(rendered, {
    ALLOWED_TAGS: allowedTags,
    ALLOWED_ATTR: ['href', 'title'],
    ALLOW_DATA_ATTR: false
  });
  const template = document.createElement('template');
  template.innerHTML = sanitized;
  for (const link of template.content.querySelectorAll('a')) {
    const href = link.getAttribute('href') ?? '';
    try {
      const target = new URL(href, window.location.href);
      if (target.protocol !== 'https:') {
        link.replaceWith(...Array.from(link.childNodes));
        continue;
      }
      link.setAttribute('href', target.href);
      link.setAttribute('target', '_blank');
      link.setAttribute('rel', 'noopener noreferrer');
    } catch {
      link.replaceWith(...Array.from(link.childNodes));
    }
  }
  return template.innerHTML;
}
