package repository

// Workspace-wide notices require a current accepted unrestricted membership, including stored rows.
const notificationRecipientVisibleSQL = `(
	(n.organization_id IS NULL AND n.category = 'security_new_signin')
	OR (n.organization_id IS NOT NULL AND EXISTS (
		SELECT 1 FROM organization_members om
		WHERE om.organization_id = n.organization_id AND om.user_id = n.user_id
			AND om.accepted_at IS NOT NULL AND (om.role = 'owner' OR om.access_scope = 'workspace')
	))
)`
