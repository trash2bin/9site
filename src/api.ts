/** Загрузка снимков с сервера: база источник правды, JS-массива больше нет. */

import type { Photo } from "./model.js";

export interface AlbumData {
  photos: Photo[];
  albums: string[];
}

function isPhoto(value: unknown): value is Photo {
  const photo = value as Partial<Photo> | null;
  return (
    typeof photo === "object" &&
    photo !== null &&
    typeof photo.name === "string" &&
    typeof photo.url === "string" &&
    typeof photo.album === "string" &&
    typeof photo.width === "number" &&
    typeof photo.height === "number"
  );
}

/** Бросает при любой ошибке: витрина покажет «база не ответила», а не пустоту. */
export async function loadPhotos(): Promise<AlbumData> {
  const response = await fetch("/api/photos", { headers: { Accept: "application/json" } });
  if (!response.ok) {
    throw new Error(`/api/photos: ${response.status}`);
  }

  const data = (await response.json()) as Partial<AlbumData>;
  if (!Array.isArray(data.photos) || !data.photos.every(isPhoto) || !Array.isArray(data.albums)) {
    throw new Error("/api/photos: неожиданный ответ");
  }

  return { photos: data.photos, albums: data.albums.filter((album) => typeof album === "string") };
}
