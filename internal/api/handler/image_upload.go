package handler

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/imagetype"
)

// imageLimits names an upload and its caps, so every image refusal says the
// same thing in the same words and tells the person what would be accepted.
type imageLimits struct {
	label    string // "avatar", "logo", "cover", "image"
	formats  string // "PNG or JPG"
	maxBytes int64
	maxDim   int
	// allowed is the formats the bytes may be; nil means PNG or JPG.
	allowed []imagetype.Format
}

func (l imageLimits) accepts() []imagetype.Format {
	if l.allowed != nil {
		return l.allowed
	}
	return avatarFormats
}

var (
	avatarLimits     = imageLimits{label: "avatar", formats: "PNG or JPG", maxBytes: avatarMaxBytes, maxDim: avatarMaxDimension}
	appLogoLimits    = imageLimits{label: "logo", formats: "PNG or JPG", maxBytes: avatarMaxBytes, maxDim: avatarMaxDimension}
	emailImageLimits = imageLimits{label: "image", formats: "PNG, JPG, GIF or WebP", maxBytes: emailImageMaxBytes, maxDim: emailImageMaxDimension}
)

func (l imageLimits) accepted() string {
	return fmt.Sprintf("Upload a %s file up to %d MB and %dpx on each side.", l.formats, mb(l.maxBytes), l.maxDim)
}

// unreadable: the bytes do not decode as any allowed format.
func (l imageLimits) unreadable() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "image_unreadable",
		fmt.Sprintf("The %s could not be read as an image. The file looks damaged, or it is a different format than its name suggests. %s", l.label, l.accepted()))
}

// wrongFormat: the bytes are an image, but not one this upload takes.
func (l imageLimits) wrongFormat() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "image_format_not_allowed",
		fmt.Sprintf("The %s must be a %s file, and this file is a different format whatever its name says. %s", l.label, l.formats, l.accepted()))
}

func (l imageLimits) tooLarge() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "image_too_large",
		fmt.Sprintf("The %s must be %d MB or smaller. Compress or resize it and try again.", l.label, mb(l.maxBytes)))
}

func (l imageLimits) tooWide() *errx.Error {
	return errx.NewWithIdentifier(errx.BadRequest, "image_dimensions_too_large",
		fmt.Sprintf("The %s must be %dpx or smaller on each side. Resize it and try again.", l.label, l.maxDim))
}
