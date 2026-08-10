import { describe, it, expect, afterEach } from 'vitest';
import { createApp, h } from 'vue';
import TodoRow from './TodoRow.vue';

// A read-only row belongs to someone else, so it shows the author's avatar
// where an editable row shows the check toggle. The row must decide from
// hasAvatar whether to request a picture at all: it used to request one
// unconditionally and drop the <img> from an inline onerror handler, which
// the CSP stopped executing -- leaving a broken-image glyph over the initial.
const mounted = [];

function mountRow(todo) {
  const el = document.createElement('div');
  document.body.appendChild(el);
  const app = createApp({
    provide: { todoApi: { editTodo() {}, todoAction() {} } },
    setup: () => () => h(TodoRow, { todo }),
  });
  app.mount(el);
  mounted.push({ app, el });
  return {
    img: () => el.querySelector('.avatar-img'),
    avatarText: () => el.querySelector('.todo-avatar')?.textContent.trim(),
  };
}

afterEach(() => {
  for (const { app, el } of mounted.splice(0)) {
    app.unmount();
    el.remove();
  }
});

const base = { id: 't1', text: 'lec 1', done: false, removed: false, readOnly: true };

describe('TodoRow avatar', () => {
  it('renders the initial and no request when the author has no picture', () => {
    const row = mountRow({ ...base, userId: 'u1', displayName: 'Adityaw' });
    expect(row.img()).toBe(null);
    expect(row.avatarText()).toBe('A');
  });

  it('renders the picture instead of the initial when the author has one', () => {
    const row = mountRow({
      ...base, userId: 'u2', displayName: 'Sanka', hasAvatar: true, avatarVersion: 1234,
    });
    expect(row.img().getAttribute('src')).toBe('/avatar/u2?v=1234');
    expect(row.avatarText()).toBe('');
  });

  it('falls back to ? for an author with no display name', () => {
    const row = mountRow({ ...base, userId: 'u3', displayName: '' });
    expect(row.avatarText()).toBe('?');
  });

  it('shows the check toggle, not an avatar, on the viewer own rows', () => {
    const row = mountRow({ ...base, readOnly: false, userId: 'u1' });
    expect(row.avatarText()).toBe(undefined);
    expect(row.img()).toBe(null);
  });
});
