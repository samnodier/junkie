<script setup>
// Focus work map, ported from the Go "heatmap" template block. Takes the
// server's /api/... heatmap shape or the guest builder's identical output.
import { computed } from 'vue';

const props = defineProps({
  heatmap: { type: Object, required: true },
  showSummary: { type: Boolean, default: true },
});

const hours = computed(() => Math.round((props.heatmap.totalMinutes || 0) / 60));
</script>

<template>
  <div class="heatmap-chart">
    <p v-if="showSummary" class="heatmap-summary">{{ hours }} hours focused in the last year</p>
    <div class="heatmap-layout">
      <div class="heatmap-dow" aria-hidden="true">
        <span></span><span>Mon</span><span></span><span>Wed</span><span></span><span>Fri</span><span></span>
      </div>
      <div class="heatmap-main">
        <div class="heatmap-months" :style="{ '--weeks': heatmap.weeks }">
          <span
            v-for="(m, i) in heatmap.months"
            :key="i"
            class="heatmap-month"
            :style="{ '--col': m.col }"
          >{{ m.label }}</span>
        </div>
        <div class="heatmap-wrap">
          <div class="heatmap" :style="{ '--weeks': heatmap.weeks }" aria-label="Focus activity heat map">
            <template v-for="(c, i) in heatmap.cells" :key="i">
              <span v-if="c.empty" class="cell cell-empty"></span>
              <span v-else :class="`cell l${c.level}`" :title="`${c.date}: ${c.minutes} min`"></span>
            </template>
          </div>
        </div>
      </div>
    </div>
    <div class="heatmap-legend" aria-hidden="true">
      <span>Less</span>
      <span class="cell l0"></span>
      <span class="cell l1"></span>
      <span class="cell l2"></span>
      <span class="cell l3"></span>
      <span class="cell l4"></span>
      <span>More</span>
    </div>
  </div>
</template>
