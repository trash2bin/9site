package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// visitMark — место в index.html, куда подставляется счётчик визитов.
const visitMark = "<!--visits-->"

// digitCount — сколько знаков показывать. Меньшие числа добиваются нулями,
// чтобы разрядность в футере не прыгала.
const digitCount = 5

// loadCounter читает сохранённое число. Отсутствующий или битый файл —
// не повод не запуститься: начинаем с нуля.
func (a *app) loadCounter() {
	raw, err := os.ReadFile(counterPath())
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		a.log.Warn("счётчик: файл не прочитан", "err", err)
		return
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n < 0 {
		a.log.Warn("счётчик: в файле не число, начинаю с нуля", "value", strings.TrimSpace(string(raw)))
		return
	}

	a.visits = n
	a.log.Info("счётчик продолжен", "visits", n)
}

// hit увеличивает счётчик на единицу и сразу сохраняет результат.
// Ошибка записи наверх не отдаётся: страницу надо показать в любом случае.
func (a *app) hit() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.visits++
	if err := saveCounter(a.visits); err != nil {
		a.log.Warn("счётчик: не сохранён", "err", err)
	}
	return a.visits
}

// saveCounter пишет число через временный файл: переименование в пределах
// одной файловой системы атомарно, поэтому при обрыве не останется
// половинчатого файла.
func saveCounter(n int) error {
	name := counterPath()
	tmp := name + ".tmp"

	if err := os.WriteFile(tmp, []byte(strconv.Itoa(n)+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, name); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// counterHTML раскладывает число на отдельные <span>, как было в вёрстке.
func counterHTML(n int) string {
	digits := strconv.Itoa(n)
	if len(digits) < digitCount {
		digits = strings.Repeat("0", digitCount-len(digits)) + digits
	}

	var b strings.Builder
	for _, d := range digits {
		b.WriteString("<span>")
		b.WriteRune(d)
		b.WriteString("</span>")
	}
	return b.String()
}
