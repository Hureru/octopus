package op

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func moveRouteCandidateReferencesTx(tx *gorm.DB, sourceID, targetID int) error {
	if sourceID <= 0 || targetID <= 0 || sourceID == targetID {
		return nil
	}
	if err := moveHeaderPolicyScopeTx(
		tx,
		model.HeaderPolicyScopeRouteCandidate,
		sourceID,
		targetID,
	); err != nil {
		return err
	}

	var quotes []model.SiteModelPriceQuote
	if err := tx.Where("route_candidate_id = ?", sourceID).Find(&quotes).Error; err != nil {
		return err
	}
	for i := range quotes {
		targetCandidateID := targetID
		quotes[i].RouteCandidateID = &targetCandidateID
		quotes[i].RefreshIdentityKey()

		var conflict model.SiteModelPriceQuote
		err := tx.Where("identity_key = ? AND id <> ?", quotes[i].IdentityKey, quotes[i].ID).
			First(&conflict).Error
		switch {
		case err == nil:
			if err := tx.Delete(&model.SiteModelPriceQuote{}, quotes[i].ID).Error; err != nil {
				return err
			}
		case err == gorm.ErrRecordNotFound:
			if err := tx.Model(&model.SiteModelPriceQuote{}).Where("id = ?", quotes[i].ID).
				Updates(map[string]any{
					"route_candidate_id": targetID,
					"identity_key":       quotes[i].IdentityKey,
				}).Error; err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

func moveCanonicalReferencesTx(tx *gorm.DB, sourceID, targetID int) error {
	return moveHeaderPolicyScopeTx(
		tx,
		model.HeaderPolicyScopeCanonicalModel,
		sourceID,
		targetID,
	)
}

func moveHeaderPolicyScopeTx(
	tx *gorm.DB,
	scope model.HeaderPolicyScope,
	sourceID int,
	targetID int,
) error {
	if sourceID <= 0 || targetID <= 0 || sourceID == targetID {
		return nil
	}
	var source model.HeaderPolicy
	switch err := tx.Where("scope = ? AND scope_id = ?", scope, sourceID).First(&source).Error; {
	case err == gorm.ErrRecordNotFound:
		return nil
	case err != nil:
		return err
	}

	var target model.HeaderPolicy
	switch err := tx.Where("scope = ? AND scope_id = ?", scope, targetID).First(&target).Error; {
	case err == nil:
		return tx.Delete(&model.HeaderPolicy{}, source.ID).Error
	case err != gorm.ErrRecordNotFound:
		return err
	}

	updates := map[string]any{"scope_id": targetID}
	if source.Name == model.HeaderPolicyDefaultName(scope, sourceID) {
		updates["name"] = model.HeaderPolicyDefaultName(scope, targetID)
	}
	return tx.Model(&model.HeaderPolicy{}).Where("id = ?", source.ID).Updates(updates).Error
}

func deleteRouteCandidateReferencesTx(tx *gorm.DB, candidateIDs []int) error {
	candidateIDs = uniqueInts(candidateIDs)
	if len(candidateIDs) == 0 {
		return nil
	}
	if err := tx.Where(
		"scope = ? AND scope_id IN ?",
		model.HeaderPolicyScopeRouteCandidate,
		candidateIDs,
	).Delete(&model.HeaderPolicy{}).Error; err != nil {
		return err
	}
	return tx.Where("route_candidate_id IN ?", candidateIDs).
		Delete(&model.SiteModelPriceQuote{}).Error
}

func deleteCanonicalReferencesTx(tx *gorm.DB, canonicalIDs []int) error {
	canonicalIDs = uniqueInts(canonicalIDs)
	if len(canonicalIDs) == 0 {
		return nil
	}
	return tx.Where(
		"scope = ? AND scope_id IN ?",
		model.HeaderPolicyScopeCanonicalModel,
		canonicalIDs,
	).Delete(&model.HeaderPolicy{}).Error
}
