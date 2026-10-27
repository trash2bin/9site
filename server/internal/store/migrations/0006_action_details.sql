-- 0006: stable English event names, targets and results for the action journal.

ALTER TABLE actions ADD COLUMN target TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN result TEXT NOT NULL DEFAULT '';

-- Convert the first free-form journal format into stable event names.
UPDATE actions
SET action = 'auth.role.changed',
    target = substr(action, 6),
    result = 'success'
WHERE action LIKE 'роль %';

UPDATE actions
SET action = CASE action
      WHEN 'вход' THEN 'auth.login.succeeded'
      WHEN 'вход не удался' THEN 'auth.login.failed'
      WHEN 'регистрация' THEN 'auth.registered'
      WHEN 'выход' THEN 'auth.logged_out'
      WHEN 'профиль обновлён' THEN 'auth.profile.updated'
      WHEN 'запрос сброса пароля' THEN 'auth.password.reset_requested'
      WHEN 'пароль сброшен' THEN 'auth.password.reset_completed'
      WHEN 'заявка с формы Sign Up' THEN 'lead.submitted'
      ELSE action
    END,
    result = CASE action
      WHEN 'вход не удался' THEN 'failure'
      ELSE 'success'
    END
WHERE action IN (
  'вход',
  'вход не удался',
  'регистрация',
  'выход',
  'профиль обновлён',
  'запрос сброса пароля',
  'пароль сброшен',
  'заявка с формы Sign Up'
);

CREATE INDEX IF NOT EXISTS actions_event_idx ON actions (action);
CREATE INDEX IF NOT EXISTS actions_result_idx ON actions (result);
