/**
 * Данные фотоальбома: все картинки, что лежат в public/assets/img/.
 *
 * В отличие от гаражного каталога, это именно «фотоальбом-сервис» (тема 18):
 * объект фильтрации — фотография, а её характеристики — раздел (альбом), тип
 * файла и ориентация кадра. Никакой придуманной цены: признаки берутся из
 * реального набора файлов, по которому живёт сайт.
 *
 * Размеры исходников занесены рядом с именами файлов; ориентация вычисляется
 * из них, а не хранится отдельно. Все пути указывают на файлы, реально
 * лежащие в public/assets/img/.
 */
export const PHOTOS = [
    // Техника — консоли со страницы «console graveyard»:
    { name: "Nintendo 3DS", src: "console-3ds.jpg", album: "gadgets", width: 1600, height: 1200 },
    { name: "NES и контроллер", src: "console-nes-web.jpg", album: "gadgets", width: 900, height: 489 },
    { name: "Game Boy", src: "console-gameboy-web.jpg", album: "gadgets", width: 577, height: 700 },
    // Гаражные снимки и детали:
    { name: "Двигатель", src: "auto-engine.jpg", album: "garage", width: 800, height: 600 },
    { name: "Авторазбор", src: "auto-store-web.jpg", album: "garage", width: 900, height: 600 },
    { name: "Свалка моторов", src: "junkyard-engines.jpg", album: "garage", width: 1600, height: 1200 },
    { name: "Тормозной диск", src: "part-brake-web.jpg", album: "garage", width: 834, height: 700 },
    { name: "Свечи зажигания", src: "part-spark-plugs-web.jpg", album: "garage", width: 900, height: 600 },
    { name: "Запчасти россыпью", src: "car-parts.png", album: "garage", width: 1032, height: 602 },
    { name: "Схема карбюратора", src: "part-carburetor.svg", album: "garage", width: 1967, height: 1846 },
    // Гифки Tux и Linux:
    { name: "Tux без сна 16 часов", src: "tux-16hours.gif", album: "mascots", width: 115, height: 86 },
    { name: "Tux машет рукой", src: "tux-big.gif", album: "mascots", width: 127, height: 150 },
    { name: "Колония Linux", src: "tux-colony.gif", album: "mascots", width: 115, height: 108 },
    { name: "обнимашки бесплатны", src: "tux-cuddle.gif", album: "mascots", width: 150, height: 63 },
    { name: "kernel boot", src: "tux-kernelboot.gif", album: "mascots", width: 100, height: 150 },
    { name: "Tux майнкрафтит", src: "tux-minecraft.gif", album: "mascots", width: 120, height: 117 },
    { name: "Радужный Tux", src: "tux-rainbow.gif", album: "mascots", width: 120, height: 120 },
    { name: "Tux у серверов", src: "tux-servers.gif", album: "mascots", width: 140, height: 140 },
    { name: "Tux сидит", src: "tux-sit.gif", album: "mascots", width: 82, height: 110 },
    { name: "sudo rm -rf yourself", src: "tux-sudorm.gif", album: "mascots", width: 150, height: 135 },
    { name: "Tux и Windows", src: "tux-windows.gif", album: "mascots", width: 120, height: 120 },
    { name: "Tux с флагом", src: "tux-winflag.gif", album: "mascots", width: 130, height: 122 },
    // Музыкальные обложки и портреты:
    { name: "9mice — Soloist", src: "9mice-album-soloist.jpg", album: "music", width: 1000, height: 1000 },
    { name: "9mice ч/б портрет", src: "9mice-in-face.png", album: "music", width: 1000, height: 1000 },
    {
        name: "Kai Angel — shh! (синий)",
        src: "kaiangel-album-shh1.jpg",
        album: "music",
        width: 1200,
        height: 1200,
    },
    {
        name: "Kai Angel — shh! (потёки)",
        src: "kaiangel-shh!-album.jpg",
        album: "music",
        width: 1000,
        height: 1000,
    },
    {
        name: "Kai Angel — DAMAGE",
        src: "kaiangel-album-damage.png",
        album: "music",
        width: 300,
        height: 300,
    },
    // Прочее — баннерная и служебная графика Web 1.0:
    { name: "Аниме-аватар", src: "avatar-anime.gif", album: "misc", width: 2048, height: 2048 },
    {
        name: "under construction",
        src: "under-construction-real.gif",
        album: "misc",
        width: 200,
        height: 200,
    },
    { name: "construction", src: "construction-real-2.gif", album: "misc", width: 177, height: 175 },
    { name: "Искра", src: "spark.gif", album: "misc", width: 24, height: 24 },
    { name: "Кнопка-фактура", src: "texture-badge.gif", album: "misc", width: 88, height: 31 },
    { name: "Полосатая фактура", src: "texture-hyper.gif", album: "misc", width: 642, height: 122 },
    { name: "Бейдж макета", src: "badge-maket.svg", album: "misc", width: 88, height: 31 },
    { name: "Паутина", src: "cobweb.svg", album: "misc", width: 80, height: 80 },
    { name: "Паутина угол", src: "cobweb-br.svg", album: "misc", width: 80, height: 80 },
    { name: "Леопардовая фактура", src: "leopard-red.svg", album: "misc", width: 72, height: 72 },
];
