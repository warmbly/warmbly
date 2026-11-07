-- The bound on stored expiries needs no reversal: a shortened issued password stays valid until its new end.
DROP TRIGGER IF EXISTS users_initial_password_guard ON users;
DROP FUNCTION IF EXISTS users_initial_password_guard();
