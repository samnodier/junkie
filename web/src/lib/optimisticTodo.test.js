import { describe, it, expect } from 'vitest';
import { applyTodoAction } from './optimisticTodo.js';

const list = () => [
  { id: 'a', done: false, removed: false },
  { id: 'b', done: true, removed: true },
];

describe('applyTodoAction', () => {
  it('toggles done both ways', () => {
    const todos = list();
    applyTodoAction(todos, 'a', 'toggle');
    applyTodoAction(todos, 'b', 'toggle');
    expect(todos.map((t) => t.done)).toEqual([true, false]);
  });

  it('removes and restores', () => {
    const todos = list();
    applyTodoAction(todos, 'a', 'remove');
    applyTodoAction(todos, 'b', 'restore');
    expect(todos.map((t) => t.removed)).toEqual([true, false]);
  });

  it('deletes the row', () => {
    const todos = list();
    applyTodoAction(todos, 'b', 'delete');
    expect(todos.map((t) => t.id)).toEqual(['a']);
  });

  it('ignores unknown ids and actions', () => {
    const todos = list();
    applyTodoAction(todos, 'zzz', 'toggle');
    applyTodoAction(todos, 'a', 'edit');
    expect(todos).toEqual(list());
  });
});
