package db

import (
	"fmt"

	"gorm.io/gorm"
)

type providerSupplyTrigger struct {
	name      string
	table     string
	operation string
	body      string
}

var providerSupplyTriggers = []providerSupplyTrigger{
	{
		name: "trg_s2_technical_resource_insert", table: "technical_resource", operation: "INSERT",
		body: `
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM resource_provider WHERE id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END;
			SELECT CASE WHEN NEW.parent_id IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM technical_resource parent
				WHERE parent.id = NEW.parent_id AND parent.provider_id = NEW.provider_id AND parent.type = 'agent'
			) THEN RAISE(ABORT, 'S2_TECHNICAL_RESOURCE_PARENT_MISMATCH') END;`,
	},
	{
		name: "trg_s2_technical_resource_update", table: "technical_resource", operation: "UPDATE",
		body: `
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM resource_provider WHERE id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END;
			SELECT CASE WHEN NEW.parent_id IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM technical_resource parent
				WHERE parent.id = NEW.parent_id AND parent.provider_id = NEW.provider_id AND parent.type = 'agent'
			) THEN RAISE(ABORT, 'S2_TECHNICAL_RESOURCE_PARENT_MISMATCH') END;`,
	},
	{
		name: "trg_s2_technical_resource_binding_insert", table: "technical_resource_binding", operation: "INSERT",
		body: technicalResourceBindingTriggerBody,
	},
	{
		name: "trg_s2_technical_resource_binding_update", table: "technical_resource_binding", operation: "UPDATE",
		body: technicalResourceBindingTriggerBody,
	},
	{
		name: "trg_s2_supply_inventory_receipt_insert", table: "supply_inventory_receipt", operation: "INSERT",
		body: `SELECT CASE WHEN NOT EXISTS (
			SELECT 1 FROM technical_resource WHERE id = NEW.technical_resource_id
		) THEN RAISE(ABORT, 'S2_TECHNICAL_RESOURCE_NOT_FOUND') END;`,
	},
	{
		name: "trg_s2_supply_inventory_receipt_update", table: "supply_inventory_receipt", operation: "UPDATE",
		body: `SELECT CASE WHEN NOT EXISTS (
			SELECT 1 FROM technical_resource WHERE id = NEW.technical_resource_id
		) THEN RAISE(ABORT, 'S2_TECHNICAL_RESOURCE_NOT_FOUND') END;`,
	},
	{
		name: "trg_s2_supply_candidate_insert", table: "supply_candidate", operation: "INSERT",
		body: `
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM resource_provider WHERE id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END;
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM technical_resource
				WHERE id = NEW.technical_resource_id AND provider_id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_CANDIDATE_SOURCE_PROVIDER_MISMATCH') END;
			SELECT CASE WHEN NEW.reviewed_by_user_id IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM user WHERE id = NEW.reviewed_by_user_id
			) THEN RAISE(ABORT, 'S2_REVIEW_USER_NOT_FOUND') END;`,
	},
	{
		name: "trg_s2_supply_candidate_update", table: "supply_candidate", operation: "UPDATE",
		body: `
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM resource_provider WHERE id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END;
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM technical_resource
				WHERE id = NEW.technical_resource_id AND provider_id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_CANDIDATE_SOURCE_PROVIDER_MISMATCH') END;
			SELECT CASE WHEN NEW.reviewed_by_user_id IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM user WHERE id = NEW.reviewed_by_user_id
			) THEN RAISE(ABORT, 'S2_REVIEW_USER_NOT_FOUND') END;`,
	},
	{
		name: "trg_s2_platform_resource_insert", table: "platform_resource", operation: "INSERT",
		body: `SELECT CASE WHEN NOT EXISTS (
			SELECT 1 FROM resource_provider WHERE id = NEW.provider_id
		) THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END;`,
	},
	{
		name: "trg_s2_platform_resource_update", table: "platform_resource", operation: "UPDATE",
		body: `SELECT CASE WHEN NOT EXISTS (
			SELECT 1 FROM resource_provider WHERE id = NEW.provider_id
		) THEN RAISE(ABORT, 'S2_PROVIDER_NOT_FOUND') END;`,
	},
	{
		name: "trg_s2_platform_resource_source_insert", table: "platform_resource_source", operation: "INSERT",
		body: `
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM platform_resource resource
				WHERE resource.id = NEW.platform_resource_id AND resource.provider_id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_RESOURCE_SOURCE_PROVIDER_MISMATCH') END;
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM supply_candidate candidate
				JOIN platform_resource resource ON resource.id = NEW.platform_resource_id
				WHERE candidate.id = NEW.supply_candidate_id
					AND candidate.provider_id = NEW.provider_id
					AND candidate.resource_type = resource.type
			) THEN RAISE(ABORT, 'S2_RESOURCE_SOURCE_CANDIDATE_MISMATCH') END;`,
	},
	{
		name: "trg_s2_platform_resource_source_update", table: "platform_resource_source", operation: "UPDATE",
		body: `
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM platform_resource resource
				WHERE resource.id = NEW.platform_resource_id AND resource.provider_id = NEW.provider_id
			) THEN RAISE(ABORT, 'S2_RESOURCE_SOURCE_PROVIDER_MISMATCH') END;
			SELECT CASE WHEN NOT EXISTS (
				SELECT 1 FROM supply_candidate candidate
				JOIN platform_resource resource ON resource.id = NEW.platform_resource_id
				WHERE candidate.id = NEW.supply_candidate_id
					AND candidate.provider_id = NEW.provider_id
					AND candidate.resource_type = resource.type
			) THEN RAISE(ABORT, 'S2_RESOURCE_SOURCE_CANDIDATE_MISMATCH') END;`,
	},
	{
		name: "trg_s2_namespace_observation_insert", table: "namespace_observation", operation: "INSERT",
		body: `SELECT CASE WHEN NOT EXISTS (
			SELECT 1 FROM platform_resource
			WHERE id = NEW.cluster_resource_id AND provider_id = NEW.provider_id AND type = 'kubernetes'
		) THEN RAISE(ABORT, 'S2_NAMESPACE_CLUSTER_PROVIDER_MISMATCH') END;`,
	},
	{
		name: "trg_s2_namespace_observation_update", table: "namespace_observation", operation: "UPDATE",
		body: `SELECT CASE WHEN NOT EXISTS (
			SELECT 1 FROM platform_resource
			WHERE id = NEW.cluster_resource_id AND provider_id = NEW.provider_id AND type = 'kubernetes'
		) THEN RAISE(ABORT, 'S2_NAMESPACE_CLUSTER_PROVIDER_MISMATCH') END;`,
	},
	{
		name: "trg_s2_resource_scope_insert", table: "resource_scope", operation: "INSERT",
		body: resourceScopeTriggerBody,
	},
	{
		name: "trg_s2_resource_scope_update", table: "resource_scope", operation: "UPDATE",
		body: resourceScopeTriggerBody,
	},
}

const technicalResourceBindingTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM technical_resource WHERE id = NEW.technical_resource_id
	) THEN RAISE(ABORT, 'S2_TECHNICAL_RESOURCE_NOT_FOUND') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM user WHERE id = NEW.bound_by_user_id
	) THEN RAISE(ABORT, 'S2_BINDING_USER_NOT_FOUND') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM technical_resource resource
		WHERE resource.id = NEW.technical_resource_id
			AND resource.credential_revision = NEW.credential_revision
	) THEN RAISE(ABORT, 'S2_BINDING_CREDENTIAL_REVISION_MISMATCH') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM technical_resource resource
		WHERE resource.id = NEW.technical_resource_id
			AND ((resource.type = 'agent' AND NEW.source_type = 'legacy_node')
				OR (resource.type = 'endpoint' AND NEW.source_type = 'legacy_endpoint'))
	) THEN RAISE(ABORT, 'S2_BINDING_SOURCE_TYPE_MISMATCH') END;
	SELECT CASE WHEN NEW.source_type = 'legacy_node' AND NOT EXISTS (
		SELECT 1 FROM node WHERE CAST(id AS TEXT) = NEW.source_id AND type = 'agent'
	) THEN RAISE(ABORT, 'S2_LEGACY_BINDING_SOURCE_NOT_FOUND') END;
	SELECT CASE WHEN NEW.source_type = 'legacy_endpoint' AND NOT EXISTS (
		SELECT 1 FROM endpoint WHERE id = NEW.source_id
	) THEN RAISE(ABORT, 'S2_LEGACY_BINDING_SOURCE_NOT_FOUND') END;
	SELECT CASE WHEN NEW.enabled = 1 AND EXISTS (
		SELECT 1 FROM technical_resource WHERE id = NEW.technical_resource_id AND type = 'endpoint'
	) AND NOT EXISTS (
		SELECT 1
		FROM technical_resource endpoint_resource
		JOIN technical_resource_binding parent_binding
			ON parent_binding.technical_resource_id = endpoint_resource.parent_id
			AND parent_binding.enabled = 1
		WHERE endpoint_resource.id = NEW.technical_resource_id
	) THEN RAISE(ABORT, 'S2_ENDPOINT_PARENT_UNBOUND') END;`

const resourceScopeTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM platform_resource
		WHERE id = NEW.platform_resource_id AND provider_id = NEW.provider_id AND type = 'kubernetes'
	) THEN RAISE(ABORT, 'S2_SCOPE_RESOURCE_PROVIDER_MISMATCH') END;
	SELECT CASE WHEN NEW.parent_id IS NOT NULL AND NOT EXISTS (
		SELECT 1 FROM resource_scope parent
		WHERE parent.id = NEW.parent_id
			AND parent.provider_id = NEW.provider_id
			AND parent.platform_resource_id = NEW.platform_resource_id
			AND parent.type = 'cluster'
	) THEN RAISE(ABORT, 'S2_SCOPE_PARENT_MISMATCH') END;
	SELECT CASE WHEN NEW.namespace_observation_id IS NOT NULL AND NOT EXISTS (
		SELECT 1 FROM namespace_observation observation
		WHERE observation.id = NEW.namespace_observation_id
			AND observation.provider_id = NEW.provider_id
			AND observation.cluster_resource_id = NEW.platform_resource_id
	) THEN RAISE(ABORT, 'S2_SCOPE_OBSERVATION_MISMATCH') END;`

func ensureProviderSupplyConstraints(database *gorm.DB) error {
	return database.Transaction(func(tx *gorm.DB) error {
		for _, trigger := range providerSupplyTriggers {
			statement := fmt.Sprintf(
				"CREATE TRIGGER IF NOT EXISTS %s BEFORE %s ON %s FOR EACH ROW BEGIN %s END",
				trigger.name,
				trigger.operation,
				trigger.table,
				trigger.body,
			)
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("create Provider supply constraint trigger %s: %w", trigger.name, err)
			}
		}
		return nil
	})
}
