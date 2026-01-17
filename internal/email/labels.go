package email

var (
	LABEL_INBOX     = "INBOX"
	LABEL_STARRED   = "STARRED"
	LABEL_IMPORTANT = "IMPORTANT"
	LABEL_UNREAD    = "UNREAD"
	LABEL_SENT      = "SENT"
	LABEL_SPAM      = "SPAM"
	LABEL_TRASH     = "TRASH"
)

var Labels []string = []string{
	LABEL_INBOX,
	LABEL_STARRED,
	LABEL_IMPORTANT,
	LABEL_UNREAD,
	LABEL_SENT,
	LABEL_SPAM,
}

func IsValidLabel(label string) bool {
	for l := range Labels {
		if Labels[l] == label {
			return true
		}
	}
	return false
}

func FindLabels(labels []string) []string {
	var l []string = make([]string, 0)
	for lab := range labels {
		for _, l2 := range Labels {
			if labels[lab] == l2 {
				l = append(l, labels[lab])
			}
		}
	}
	return l
}

func Contains(data *[]string, name string) bool {
	for _, s := range *data {
		if s == name {
			return true
		}
	}
	return false
}

type FlagProps struct {
	Reverse bool
	Label   string
}

var flags map[string]FlagProps = map[string]FlagProps{
	"\\Seen": {
		Reverse: true,
		Label:   LABEL_UNREAD,
	},
	"\\Flagged": {
		Reverse: false,
		Label:   LABEL_STARRED,
	},
	"\\Deleted": {
		Reverse: false,
		Label:   LABEL_TRASH,
	},
	"\\Important": {
		Reverse: false,
		Label:   LABEL_IMPORTANT,
	},
}

func ActiveFlag(flag string) bool {
	_, ok := flags[flag]
	return ok
}

func LabelizeFlags(sflags []string) []string {
	var labels []string

	for k, f := range flags {
		if f.Reverse {
			if !Contains(&sflags, k) {
				labels = append(labels, f.Label)
			}
		} else {
			if Contains(&sflags, k) {
				labels = append(labels, f.Label)
			}
		}
	}

	return labels
}
