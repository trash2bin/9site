/** Единое описание критериев: его обходят фильтр и интерфейс. */
import { fileTypeOf, orientationOf } from "./media.js";
import { ALBUM_OPTIONS, ORIENTATION_OPTIONS, TYPE_OPTIONS } from "./model.js";
export const CRITERIA = [
    { key: "album", title: "Раздел", options: ALBUM_OPTIONS, valueOf: (photo) => photo.album },
    { key: "type", title: "Тип файла", options: TYPE_OPTIONS, valueOf: fileTypeOf },
    { key: "orientation", title: "Ориентация", options: ORIENTATION_OPTIONS, valueOf: orientationOf },
];
/** Проверяет, что строка — имя описанного критерия. */
export function isCriterionKey(value) {
    return CRITERIA.some(({ key }) => key === value);
}
