/** Связывает данные из базы, фильтр, интерфейс и режим печати страницы альбома. */

import { loadPhotos } from "./api.js";
import { criteriaFor, type CriterionDefinition } from "./criteria.js";
import { getAvailability, applyFilters } from "./filter.js";
import type { Photo } from "./model.js";
import { initPrintMode } from "./print.js";
import { readState, renderCount, renderFilters, renderPhotos, updateFilters } from "./render.js";

const FILTERS = "#filters";
const PHOTOS_GRID = "#elements";
const RESULT_COUNT = "#filter-count";
const ALBUM_STATS = "[data-album-stats]";

function placeholder(text: string): void {
  const host = document.querySelector(PHOTOS_GRID);
  if (host) host.innerHTML = `<p class="catalog-empty">${text}</p>`;
}

async function init(): Promise<void> {
  initPrintMode();

  let photos: Photo[];
  let criteria: readonly CriterionDefinition[];
  try {
    const data = await loadPhotos();
    photos = data.photos;
    criteria = criteriaFor(data.albums, data.tags);
  } catch (error) {
    // Пустая сетка выглядела бы как «ничего не нашлось», поэтому текст другой.
    console.error(error);
    placeholder("[ база не ответила // обнови страницу ]");
    return;
  }

  const stats = document.querySelector(ALBUM_STATS);
  if (stats) {
    const tags = criteria.find((criterion) => criterion.key === "tag")?.options.length ?? 0;
    const tagStats = tags > 0 ? ` ★ ${tags} ТЕГОВ` : "";
    stats.textContent = `★ ${photos.length} СНИМКОВ ★ ${criteria[0].options.length} РАЗДЕЛОВ${tagStats} ★ 3 ТИПА ★ 3 ОРИЕНТАЦИИ ★`;
  }

  function refresh(): void {
    const state = readState(FILTERS);
    const matchingPhotos = applyFilters(photos, state, criteria);
    renderPhotos(matchingPhotos, PHOTOS_GRID);
    renderCount(matchingPhotos.length, photos.length, RESULT_COUNT);
    updateFilters(state, getAvailability(photos, state, criteria), FILTERS);
  }

  renderFilters(criteria, FILTERS);
  refresh();

  document.querySelector(FILTERS)?.addEventListener("change", refresh);
  document.querySelector("[data-filter-reset]")?.addEventListener("click", () => {
    for (const checkbox of document.querySelectorAll<HTMLInputElement>(`${FILTERS} input[type=checkbox]`)) {
      checkbox.checked = false;
    }
    refresh();
  });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => void init());
} else {
  void init();
}
