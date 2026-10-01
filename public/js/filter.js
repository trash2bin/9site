/** Умная фильтрация и доступность значений фотоальбома. */
import { CRITERIA } from "./criteria.js";
/** Проверяет, подходит ли снимок под все выбранные значения. */
function matches(photo, state) {
    return CRITERIA.every(({ key, valueOf }) => {
        const selected = state[key] ?? [];
        return selected.length === 0 || selected.includes(valueOf(photo));
    });
}
/** Выбирает снимки, совпадающие с текущим состоянием фильтра. */
export function applyFilters(photos, state) {
    return photos.filter((photo) => matches(photo, state));
}
/**
 * Для каждого варианта проверяет, найдётся ли хотя бы один снимок при
 * текущих выборах в остальных группах. Результат используется для disabled.
 */
export function getAvailability(photos, state) {
    const availability = {};
    for (const criterion of CRITERIA) {
        availability[criterion.key] = new Set(criterion.options
            .filter(({ value }) => {
            const probe = { ...state, [criterion.key]: [value] };
            return photos.some((photo) => matches(photo, probe));
        })
            .map(({ value }) => value));
    }
    return availability;
}
