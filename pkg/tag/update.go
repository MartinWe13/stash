package tag

import (
	"fmt"
	"github.com/stashapp/stash/pkg/models"
)

type NameExistsError struct {
	Name string
}

func (e *NameExistsError) Error() string {
	return fmt.Sprintf("tag with name '%s' already exists", e.Name)
}

type NameUsedByAliasError struct {
	Name     string
	OtherTag string
}

func (e *NameUsedByAliasError) Error() string {
	return fmt.Sprintf("name '%s' is used as alias for '%s'", e.Name, e.OtherTag)
}

type InvalidTagHierarchyError struct {
	Direction   string
	InvalidTag  string
	ApplyingTag string
}

func (e *InvalidTagHierarchyError) Error() string {
	if e.InvalidTag == e.ApplyingTag {
		return fmt.Sprintf("Cannot apply tag \"%s\" as it already is a %s", e.InvalidTag, e.Direction)
	} else {
		return fmt.Sprintf("Cannot apply tag \"%s\" as it is linked to \"%s\" which already is a %s", e.ApplyingTag, e.InvalidTag, e.Direction)
	}
}

// EnsureTagNameUnique returns an error if the tag name provided
// is used as a name or alias of another existing tag.
func EnsureTagNameUnique(id int, name string, qb models.TagReader) error {
	// ensure name is unique
	sameNameTag, err := ByName(qb, name)
	if err != nil {
		return err
	}

	if sameNameTag != nil && id != sameNameTag.ID {
		return &NameExistsError{
			Name: name,
		}
	}

	// query by alias
	sameNameTag, err = ByAlias(qb, name)
	if err != nil {
		return err
	}

	if sameNameTag != nil && id != sameNameTag.ID {
		return &NameUsedByAliasError{
			Name:     name,
			OtherTag: sameNameTag.Name,
		}
	}

	return nil
}

func EnsureAliasesUnique(id int, aliases []string, qb models.TagReader) error {
	for _, a := range aliases {
		if err := EnsureTagNameUnique(id, a, qb); err != nil {
			return err
		}
	}

	return nil
}

func EnsureUniqueHierarchy(id int, parentIDs, childIDs []int, qb models.TagReader) error {
	allAncestors := make(map[int]*models.Tag)
	allDescendants := make(map[int]*models.Tag)
	excludeIDs := []int{id}

	validateParent := func(testID, applyingID int) error {
		if parentTag, exists := allAncestors[testID]; exists {
			applyingTag, err := qb.Find(applyingID)

			if err != nil {
				return nil
			}

			return &InvalidTagHierarchyError{
				Direction:   "parent",
				InvalidTag:  parentTag.Name,
				ApplyingTag: applyingTag.Name,
			}
		}

		return nil
	}

	validateChild := func(testID, applyingID int) error {
		if childTag, exists := allDescendants[testID]; exists {
			applyingTag, err := qb.Find(applyingID)

			if err != nil {
				return nil
			}

			return &InvalidTagHierarchyError{
				Direction:   "child",
				InvalidTag:  childTag.Name,
				ApplyingTag: applyingTag.Name,
			}
		}

		return validateParent(testID, applyingID)
	}

	if parentIDs == nil {
		parentTags, err := qb.FindByChildTagID(id)
		if err != nil {
			return err
		}

		for _, parentTag := range parentTags {
			parentIDs = append(parentIDs, parentTag.ID)
		}
	}

	if childIDs == nil {
		childTags, err := qb.FindByParentTagID(id)
		if err != nil {
			return err
		}

		for _, childTag := range childTags {
			childIDs = append(childIDs, childTag.ID)
		}
	}

	for _, parentID := range parentIDs {
		parentsAncestors, err := qb.FindAllAncestors(parentID, excludeIDs)
		if err != nil {
			return err
		}

		for _, ancestorTag := range parentsAncestors {
			if err := validateParent(ancestorTag.ID, parentID); err != nil {
				return err
			}

			allAncestors[ancestorTag.ID] = ancestorTag
		}
	}

	for _, childID := range childIDs {
		childsDescendants, err := qb.FindAllDescendants(childID, excludeIDs)
		if err != nil {
			return err
		}

		for _, descendentTag := range childsDescendants {
			if err := validateChild(descendentTag.ID, childID); err != nil {
				return err
			}

			allDescendants[descendentTag.ID] = descendentTag
		}
	}

	return nil
}

func MergeHierarchy(destination int, sources []int, qb models.TagReader) ([]int, []int, error) {
	var mergedParents, mergedChildren []int
	allIds := append([]int{destination}, sources...)

	addTo := func(mergedItems []int, tags []*models.Tag) []int {
	Tags:
		for _, tag := range tags {
			// Ignore tags which are already set
			for _, existingItem := range mergedItems {
				if tag.ID == existingItem {
					continue Tags
				}
			}

			// Ignore tags which are being merged, as these are rolled up anyway (if A is merged into B any direct link between them can be ignored)
			for _, id := range allIds {
				if tag.ID == id {
					continue Tags
				}
			}

			mergedItems = append(mergedItems, tag.ID)
		}

		return mergedItems
	}

	for _, id := range allIds {
		parents, err := qb.FindByChildTagID(id)
		if err != nil {
			return nil, nil, err
		}

		mergedParents = addTo(mergedParents, parents)

		children, err := qb.FindByParentTagID(id)
		if err != nil {
			return nil, nil, err
		}

		mergedChildren = addTo(mergedChildren, children)
	}

	err := EnsureUniqueHierarchy(destination, mergedParents, mergedChildren, qb)
	if err != nil {
		return nil, nil, err
	}

	return mergedParents, mergedChildren, nil
}
