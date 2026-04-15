# SCR (Session Completion Ratio) — RFC 6076 §4.9

## Формула

```
SCR = (# of Successfully Completed Sessions) / (Total # of Session Requests) × 100
```

- **Числитель**: диалоги, прошедшие полный цикл INVITE→200 OK→BYE→200 OK
- **Знаменатель**: Total INVITE (3xx НЕ исключаются, как в ISA)
- Undefined при 0 INVITE

## Подзадачи

- [x] 1. Добавить поле `sessionCompletedTotal int64` в `metrics` struct и метод `SessionCompleted()` в `Metricser` + реализацию
- [x] 2. Реализовать `newSCR()` — GaugeFunc `sip_exporter_scr`
- [x] 3. Вызвать `SessionCompleted()` из `exporter.handleMessage()` при 200 OK на BYE (рядом с `dialoger.Delete`)
- [x] 4. Юнит-тесты SCR в `internal/service/metrics_test.go`
- [x] 5. E2E-тест SCR в `test/e2e/scr_test.go`
- [x] 6. Обновить AGENTS.md: добавить SCR в формулы и архитектурные заметки
- [x] 7. Прогнать lint + test
