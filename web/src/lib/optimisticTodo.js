// Apply a todo action to a local list straight away, before the server
// answers. Every one of these is author-only and has a single possible
// outcome, so the screen can show it on click; the refresh that follows the
// request replaces the guess with the server's state either way, which is
// also what undoes it if the request was refused.
export function applyTodoAction(list, id, action) {
  const index = list.findIndex((t) => t.id === id);
  if (index === -1) return;
  const todo = list[index];
  if (action === 'toggle') todo.done = !todo.done;
  else if (action === 'remove') todo.removed = true;
  else if (action === 'restore') todo.removed = false;
  else if (action === 'delete') list.splice(index, 1);
}
