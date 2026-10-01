/** Вычисляемые признаки файла: тип и пропорции не хранятся в данных. */

import type { FileType, Orientation, Photo } from "./model.js";

export function fileTypeOf(photo: Photo): FileType {
  const extension = photo.src.split(".").pop()?.toLowerCase();
  if (extension === "gif") return "gif";
  if (extension === "svg") return "vector";
  return "photo";
}

export function orientationOf(photo: Photo): Orientation {
  const ratio = photo.width / photo.height;
  if (ratio >= 1.2) return "wide";
  if (ratio <= 1 / 1.2) return "tall";
  return "square";
}
