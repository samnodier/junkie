<script setup>
// Room todos grouped into "you" and "everyone else", ported from the
// todo-groups templates, with the collapsible group headers.
//
// "Everyone else" is grouped per person rather than being one flat list. In a
// room of three the difference is cosmetic; in a room of a hundred it is what
// keeps the panel usable, because the number of rows you scroll past tracks
// the number of *people* rather than the number of todos. Each person's list
// is collapsed until you open it, so you read the room by who is in it.
import { computed, ref } from 'vue';
import TodoRow from './TodoRow.vue';

const props = defineProps({
  roomCode: { type: String, required: true },
  userName: { type: String, default: '' },
  mine: { type: Array, default: () => [] },
  others: { type: Array, default: () => [] },
});

const mineOpen = ref(true);
// "Everyone else" starts collapsed so your own list stays front and center.
const othersOpen = ref(false);
// Which people inside it are expanded, by user id.
const openPeople = ref(new Set());

function togglePerson(id) {
  const next = new Set(openPeople.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  openPeople.value = next;
}

// The server already orders todos (unfinished first, newest first), so
// grouping preserves that order within each person and takes each person's
// first appearance as their place in the list.
const people = computed(() => {
  const byPerson = new Map();
  for (const todo of props.others) {
    const id = todo.userId || todo.displayName;
    if (!byPerson.has(id)) {
      byPerson.set(id, {
        id,
        name: todo.displayName,
        hasAvatar: todo.hasAvatar,
        avatarVersion: todo.avatarVersion,
        todos: [],
      });
    }
    byPerson.get(id).todos.push(todo);
  }
  return [...byPerson.values()];
});

const outstanding = (todos) => todos.filter((t) => !t.done && !t.removed).length;
const initial = (name) => (name ? name[0].toUpperCase() : '?');
</script>

<template>
  <div class="todo-groups" :data-room="roomCode">
    <section class="todo-group" data-group="mine">
      <button type="button" class="todo-group-toggle" :aria-expanded="String(mineOpen)" :aria-controls="`todo-group-mine-${roomCode}`" @click="mineOpen = !mineOpen">
        <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true" :style="mineOpen ? '' : 'transform: rotate(-90deg)'"><path d="M6 9l6 6 6-6"/></svg>
        <span class="label">{{ userName || 'You' }}</span>
      </button>
      <ul class="todo-list" :id="`todo-group-mine-${roomCode}`" v-show="mineOpen">
        <TodoRow v-for="t in mine" :key="t.id" :todo="t" />
        <li v-if="!mine.length" class="empty">Nothing here yet.</li>
      </ul>
    </section>
    <section v-if="people.length" class="todo-group" data-group="others">
      <button type="button" class="todo-group-toggle" :aria-expanded="String(othersOpen)" :aria-controls="`todo-group-others-${roomCode}`" @click="othersOpen = !othersOpen">
        <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true" :style="othersOpen ? '' : 'transform: rotate(-90deg)'"><path d="M6 9l6 6 6-6"/></svg>
        <span class="label">Everyone else</span>
        <span class="muted todo-group-count">{{ people.length }}</span>
      </button>
      <div :id="`todo-group-others-${roomCode}`" v-show="othersOpen" class="todo-people">
        <section v-for="p in people" :key="p.id" class="todo-person">
          <button
            type="button"
            class="todo-person-toggle"
            :aria-expanded="String(openPeople.has(p.id))"
            :aria-controls="`todo-person-${roomCode}-${p.id}`"
            @click="togglePerson(p.id)"
          >
            <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true" :style="openPeople.has(p.id) ? '' : 'transform: rotate(-90deg)'"><path d="M6 9l6 6 6-6"/></svg>
            <span class="todo-avatar todo-person-avatar" aria-hidden="true"><img v-if="p.hasAvatar" class="avatar-img" :src="`/avatar/${p.id}?v=${p.avatarVersion}`" alt=""><template v-else>{{ initial(p.name) }}</template></span>
            <span class="todo-person-name">{{ p.name }}</span>
            <!-- The count is what makes a collapsed row worth reading: you can
                 see who has work outstanding without opening anyone. -->
            <span class="muted todo-person-count">{{ outstanding(p.todos) }} of {{ p.todos.length }}</span>
          </button>
          <ul class="todo-list" :id="`todo-person-${roomCode}-${p.id}`" v-show="openPeople.has(p.id)">
            <TodoRow v-for="t in p.todos" :key="t.id" :todo="t" />
          </ul>
        </section>
      </div>
    </section>
  </div>
</template>
