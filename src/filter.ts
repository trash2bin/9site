import { CRITERIA, type Availability, type FilterState } from "./criteria.js";
import type { Photo } from "./model.js";

function matches(photo: Photo, state: FilterState): boolean {
  return CRITERIA.every(({ key, valueOf }) => {
    const selected = state[key] ?? [];
    return selected.length === 0 || selected.includes(valueOf(photo));
  });
}

export function applyFilters(photos: Photo[], state: FilterState): Photo[] {
  return photos.filter((photo) => matches(photo, state));
}

/**
 * Для каждого варианта проверяет, найдётся ли снимок при выборах в остальных
 * группах; результат используется для disabled.
 */
export function getAvailability(photos: Photo[], state: FilterState): Availability {
  const availability: Availability = {};

  for (const criterion of CRITERIA) {
    availability[criterion.key] = new Set(
      criterion.options
        .filter(({ value }) => {
          const probe: FilterState = { ...state, [criterion.key]: [value] };
          return photos.some((photo) => matches(photo, probe));
        })
        .map(({ value }) => value)
    );
  }

  return availability;
}
