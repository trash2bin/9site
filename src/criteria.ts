/** Единое описание критериев: его обходят фильтр и интерфейс. */

import { fileTypeOf, orientationOf } from "./media.js";
import { ORIENTATION_OPTIONS, TYPE_OPTIONS, type Album, type Photo } from "./model.js";

export interface CriterionDefinition {
  key: string;
  title: string;
  options: readonly { value: string; label: string }[];
  /** Значения кадра по этому критерию: у тегов их несколько, у остальных одно. */
  valuesOf: (photo: Photo) => readonly string[];
}

/**
 * Разделы приходят из API фиксированным списком с подписями, теги — из базы;
 * группа тегов появляется, когда в базе есть хоть один тег.
 */
export function criteriaFor(albums: readonly Album[], tags: string[] = []): readonly CriterionDefinition[] {
  const criteria: CriterionDefinition[] = [
    {
      key: "album",
      title: "Раздел",
      options: albums.map((album) => ({ value: album.slug, label: album.label })),
      valuesOf: (photo: Photo) => [photo.album],
    },
    { key: "type", title: "Тип файла", options: TYPE_OPTIONS, valuesOf: (photo) => [fileTypeOf(photo)] },
    {
      key: "orientation",
      title: "Ориентация",
      options: ORIENTATION_OPTIONS,
      valuesOf: (photo) => [orientationOf(photo)],
    },
  ];

  if (tags.length > 0) {
    criteria.push({
      key: "tag",
      title: "Тег",
      options: tags.map((slug) => ({ value: slug, label: slug })),
      valuesOf: (photo: Photo) => photo.tags,
    });
  }

  return criteria;
}

export type CriterionKey = "album" | "type" | "orientation" | "tag";
export type FilterState = Partial<Record<CriterionKey, string[]>>;
export type Availability = Partial<Record<CriterionKey, Set<string>>>;

const CRITERION_KEYS: readonly string[] = ["album", "type", "orientation", "tag"];

export function isCriterionKey(value: string): value is CriterionKey {
  return CRITERION_KEYS.includes(value);
}
