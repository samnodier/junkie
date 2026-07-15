<script setup>
// Room todos grouped into "you" and "everyone else", ported from the
// todo-groups templates, with the collapsible group headers.
import { ref } from 'vue';
import TodoRow from './TodoRow.vue';

defineProps({
  roomCode: { type: String, required: true },
  userName: { type: String, default: '' },
  mine: { type: Array, default: () => [] },
  others: { type: Array, default: () => [] },
});

const mineOpen = ref(true);
const othersOpen = ref(true);
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
    <section v-if="others.length" class="todo-group" data-group="others">
      <button type="button" class="todo-group-toggle" :aria-expanded="String(othersOpen)" :aria-controls="`todo-group-others-${roomCode}`" @click="othersOpen = !othersOpen">
        <svg class="todo-group-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true" :style="othersOpen ? '' : 'transform: rotate(-90deg)'"><path d="M6 9l6 6 6-6"/></svg>
        <span class="label">Everyone else</span>
      </button>
      <ul class="todo-list" :id="`todo-group-others-${roomCode}`" v-show="othersOpen">
        <TodoRow v-for="t in others" :key="t.id" :todo="t" />
      </ul>
    </section>
  </div>
</template>
