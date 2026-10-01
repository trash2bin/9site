/** Связывает данные, фильтр, интерфейс и режим печати страницы альбома. */
import { getAvailability, applyFilters } from "./filter.js";
import { PHOTOS } from "./photos.js";
import { initPrintMode } from "./print.js";
import { readState, renderCount, renderFilters, renderPhotos, updateFilters } from "./render.js";
const FILTERS = "#filters";
const PHOTOS_GRID = "#elements";
const RESULT_COUNT = "#filter-count";
/** Применяет состояние и обновляет карточки, счётчик и доступность чекбоксов. */
function refresh() {
    const state = readState(FILTERS);
    const matchingPhotos = applyFilters(PHOTOS, state);
    renderPhotos(matchingPhotos, PHOTOS_GRID);
    renderCount(matchingPhotos.length, PHOTOS.length, RESULT_COUNT);
    updateFilters(state, getAvailability(PHOTOS, state), FILTERS);
}
function init() {
    initPrintMode();
    renderFilters(FILTERS);
    refresh();
    document.querySelector(FILTERS)?.addEventListener("change", refresh);
    document.querySelector("[data-filter-reset]")?.addEventListener("click", () => {
        for (const checkbox of document.querySelectorAll(`${FILTERS} input[type=checkbox]`)) {
            checkbox.checked = false;
        }
        refresh();
    });
}
if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
}
else {
    init();
}
