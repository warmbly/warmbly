-- Reverses 000151 by clearing bit 22 again. Safe in both directions: the bit
-- gates only the tester routes, which do not exist without the code that
-- reads it.
UPDATE users
SET admin_permissions = admin_permissions & ~4194304,
    updated_at        = NOW()
WHERE admin_permissions & 4194304 <> 0;
