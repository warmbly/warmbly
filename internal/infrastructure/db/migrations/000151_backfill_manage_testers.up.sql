-- Give bit 22 (manage_testers) to the admins who already hold everything else.
--
-- users.admin_permissions stores the raw mask, so widening
-- models.AllAdminPermissions does not touch existing rows: a super admin
-- granted before this bit existed would get a 403 from the tester routes and
-- no explanation. Bits are never reused here, so a new one always needs this.
--
-- Scoped to masks that already contain every previously live permission
-- (4194303 = (1 << 22) - 1). An admin with a narrower grant is left alone:
-- they were deliberately given less, and creating accounts is more than any
-- of the permissions they hold.
UPDATE users
SET admin_permissions = admin_permissions | 4194304,
    updated_at        = NOW()
WHERE admin_permissions & 4194303 = 4194303
  AND admin_permissions & 4194304 = 0;
