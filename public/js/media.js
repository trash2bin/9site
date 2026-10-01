/** Вычисляемые признаки файла: тип и пропорции не хранятся в данных. */
export function fileTypeOf(photo) {
    const extension = photo.src.split(".").pop()?.toLowerCase();
    if (extension === "gif")
        return "gif";
    if (extension === "svg")
        return "vector";
    return "photo";
}
export function orientationOf(photo) {
    const ratio = photo.width / photo.height;
    if (ratio >= 1.2)
        return "wide";
    if (ratio <= 1 / 1.2)
        return "tall";
    return "square";
}
