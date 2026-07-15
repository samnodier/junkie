<script setup>
// One todo row, ported from the todo-row template: check toggle, click-text-
// to-edit (900ms autosave, Enter/blur commits, Escape reverts), remove /
// restore / delete. Read-only rows show the author instead of controls.
import { nextTick, ref } from 'vue';
import { useDeskStore } from '@/stores/desk';

const props = defineProps({
  todo: { type: Object, required: true },
});
const desk = useDeskStore();

const editing = ref(false);
const editValue = ref('');
const editInput = ref(null);
let original = '';
let lastSaved = '';
let saveTimer = null;
let finished = false;

async function startEdit() {
  if (props.todo.readOnly || props.todo.done || props.todo.removed) return;
  original = props.todo.text;
  lastSaved = original;
  editValue.value = original;
  editing.value = true;
  finished = false;
  await nextTick();
  editInput.value?.focus();
  editInput.value?.setSelectionRange(editValue.value.length, editValue.value.length);
}
function onEditInput() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    const text = editValue.value.trim();
    if (text !== '' && text !== lastSaved) {
      lastSaved = text;
      desk.editTodo(props.todo.id, text, false);
    }
  }, 900);
}
function finish(text, needsSave) {
  if (finished) return;
  finished = true;
  clearTimeout(saveTimer);
  editing.value = false;
  if (needsSave) desk.editTodo(props.todo.id, text, true);
}
function commit() {
  const next = editValue.value.trim();
  if (next === '') {
    // Emptied text reverts, undoing anything autosave pushed.
    finish(original, lastSaved !== original);
    return;
  }
  finish(next, next !== lastSaved);
}
function revert() {
  finish(original, lastSaved !== original);
}

const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <li :class="todo.removed ? 'removed' : todo.done ? 'done' : ''">
    <span v-if="todo.readOnly" class="todo-avatar" aria-hidden="true"><img class="avatar-img" :src="`/avatar/${todo.userId}`" alt="" loading="lazy" onerror="this.remove()">{{ initial(todo.displayName) }}</span>
    <button
      v-else
      type="button"
      class="check"
      :aria-label="todo.done ? 'Mark incomplete' : 'Mark complete'"
      @click="desk.todoAction(todo.id, 'toggle')"
    >{{ todo.done ? '✓' : '○' }}</button>

    <span>
      <template v-if="todo.readOnly">
        <span class="todo-text">{{ todo.text }}</span><span class="todo-sep" aria-hidden="true"> · </span><span class="todo-author">{{ todo.displayName }}</span>
      </template>
      <template v-else>
        <span
          :class="!todo.removed && !todo.done ? 'todo-editable' : ''"
          :data-id="todo.id"
          :title="!todo.removed && !todo.done ? 'Click to edit' : undefined"
          @click="startEdit"
        >
          <input
            v-if="editing"
            ref="editInput"
            type="text"
            class="todo-edit-input"
            maxlength="500"
            v-model="editValue"
            @input="onEditInput"
            @keydown.enter.prevent="commit"
            @keydown.esc="revert"
            @blur="commit"
          >
          <template v-else>{{ todo.text }}</template>
        </span>
      </template>
    </span>

    <template v-if="!todo.readOnly">
      <div v-if="todo.removed" class="todo-actions">
        <button type="button" class="todo-action todo-restore" title="Bring back" aria-label="Bring back" @click="desk.todoAction(todo.id, 'restore')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 14 4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/></svg></button>
        <button type="button" class="todo-action todo-delete" title="Delete permanently" aria-label="Delete permanently" @click="desk.todoAction(todo.id, 'delete')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg></button>
      </div>
      <button v-else type="button" class="todo-action todo-remove" title="Remove" aria-label="Remove" @click="desk.todoAction(todo.id, 'remove')"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button>
    </template>
  </li>
</template>
