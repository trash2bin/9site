/** Единое описание критериев: его обходят фильтр и интерфейс. */

import { fileTypeOf, orientationOf } from "./media.js";
import { albumLabel, ORIENTATION_OPTIONS, TYPE_OPTIONS, type Photo } from "./model.js";

export interface CriterionDefinition {
  key: string;
  title: string;
  options: readonly { value: string; label: string }[];
  valueOf: (photo: Photo) => string;
}

/** Разделы приходят из базы: фильтр строится по ним, а не по константе. */
export function criteriaFor(albums: string[]): readonly CriterionDefinition[] {
  return [
    {
      key: "album",
      title: "Раздел",
      options: albums.map((slug) => ({ value: slug, label: albumLabel(slug) })),
      valueOf: (photo: Photo) => photo.album,
    },
    { key: "type", title: "Тип файла", options: TYPE_OPTIONS, valueOf: fileTypeOf },
    { key: "orientation", title: "Ориентация", options: ORIENTATION_OPTIONS, valueOf: orientationOf },
  ];
}

export type CriterionKey = "album" | "type" | "orientation";
export type FilterState = Partial<Record<CriterionKey, string[]>>;
export type Availability = Partial<Record<CriterionKey, Set<string>>>;

const CRITERION_KEYS: readonly string[] = ["album", "type", "orientation"];

export function isCriterionKey(value: string): value is CriterionKey {
  return CRITERION_KEYS.includes(value);
}
