package leadfinder

// Category is one kind of business the finder offers as a preset, mapped onto
// each provider's own vocabulary.
type Category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
	// Term is the plain search phrase for text-based providers.
	Term string `json:"-"`
	// OSM are Overpass tag selectors; a place matching any of them counts.
	OSM []string `json:"-"`
	// GoogleType is a Places (New) Table A type, empty to search by Term only.
	GoogleType string `json:"-"`
}

func cat(id, label, group, term, googleType string, osm ...string) Category {
	return Category{ID: id, Label: label, Group: group, Term: term, GoogleType: googleType, OSM: osm}
}

// Categories is every preset, in the order the dashboard lists them.
var Categories = []Category{
	cat("restaurant", "Restaurants", "Food and drink", "restaurant", "restaurant", `["amenity"="restaurant"]`),
	cat("cafe", "Cafes", "Food and drink", "cafe", "cafe", `["amenity"="cafe"]`),
	cat("bar", "Bars and pubs", "Food and drink", "bar", "bar", `["amenity"~"^(bar|pub)$"]`),
	cat("bakery", "Bakeries", "Food and drink", "bakery", "bakery", `["shop"="bakery"]`),
	cat("fast_food", "Fast food", "Food and drink", "fast food restaurant", "fast_food_restaurant", `["amenity"="fast_food"]`),
	cat("catering", "Caterers", "Food and drink", "catering", "", `["craft"="caterer"]`, `["shop"="catering"]`),

	cat("dentist", "Dentists", "Health", "dentist", "dentist", `["amenity"="dentist"]`, `["healthcare"="dentist"]`),
	cat("doctor", "Doctors", "Health", "doctor", "doctor", `["amenity"="doctors"]`, `["healthcare"="doctor"]`),
	cat("clinic", "Clinics", "Health", "medical clinic", "", `["amenity"="clinic"]`, `["healthcare"="clinic"]`),
	cat("pharmacy", "Pharmacies", "Health", "pharmacy", "pharmacy", `["amenity"="pharmacy"]`),
	cat("veterinary", "Veterinarians", "Health", "veterinarian", "veterinary_care", `["amenity"="veterinary"]`),
	cat("physio", "Physiotherapists", "Health", "physiotherapist", "physiotherapist", `["healthcare"="physiotherapist"]`),
	cat("chiropractor", "Chiropractors", "Health", "chiropractor", "chiropractor", `["healthcare"="chiropractor"]`, `["healthcare:speciality"="chiropractic"]`),
	cat("optician", "Opticians", "Health", "optician", "", `["shop"="optician"]`, `["healthcare"="optometrist"]`),

	cat("hair_salon", "Hair salons", "Beauty and fitness", "hair salon", "hair_salon", `["shop"="hairdresser"]`),
	cat("beauty_salon", "Beauty salons", "Beauty and fitness", "beauty salon", "beauty_salon", `["shop"="beauty"]`),
	cat("barber", "Barbers", "Beauty and fitness", "barber shop", "barber_shop", `["shop"="hairdresser"]["hairdresser"="barber"]`),
	cat("spa", "Spas and massage", "Beauty and fitness", "spa", "spa", `["leisure"="spa"]`, `["shop"="massage"]`, `["amenity"="spa"]`),
	cat("nail_salon", "Nail salons", "Beauty and fitness", "nail salon", "nail_salon", `["shop"="beauty"]["beauty"="nails"]`),
	cat("gym", "Gyms", "Beauty and fitness", "gym", "gym", `["leisure"="fitness_centre"]`),
	cat("yoga", "Yoga studios", "Beauty and fitness", "yoga studio", "yoga_studio", `["sport"="yoga"]`, `["leisure"="fitness_centre"]["sport"="yoga"]`),
	cat("tattoo", "Tattoo studios", "Beauty and fitness", "tattoo studio", "", `["shop"="tattoo"]`),

	cat("plumber", "Plumbers", "Home services", "plumber", "plumber", `["craft"="plumber"]`),
	cat("electrician", "Electricians", "Home services", "electrician", "electrician", `["craft"="electrician"]`),
	cat("roofer", "Roofers", "Home services", "roofing contractor", "roofing_contractor", `["craft"="roofer"]`),
	cat("hvac", "HVAC", "Home services", "hvac contractor", "", `["craft"="hvac"]`),
	cat("painter", "Painters", "Home services", "painter", "painter", `["craft"="painter"]`),
	cat("carpenter", "Carpenters", "Home services", "carpenter", "", `["craft"="carpenter"]`),
	cat("locksmith", "Locksmiths", "Home services", "locksmith", "locksmith", `["shop"="locksmith"]`, `["craft"="locksmith"]`),
	cat("landscaping", "Landscapers", "Home services", "landscaping", "", `["craft"="gardener"]`, `["shop"="garden_centre"]`),
	cat("cleaning", "Cleaning services", "Home services", "cleaning service", "", `["shop"="dry_cleaning"]`, `["office"="cleaning"]`, `["craft"="cleaning"]`),
	cat("moving", "Movers", "Home services", "moving company", "moving_company", `["office"="moving_company"]`, `["shop"="moving"]`),

	cat("lawyer", "Law firms", "Professional services", "law firm", "lawyer", `["office"="lawyer"]`),
	cat("accountant", "Accountants", "Professional services", "accountant", "accounting", `["office"="accountant"]`, `["office"="tax_advisor"]`),
	cat("insurance", "Insurance agents", "Professional services", "insurance agency", "insurance_agency", `["office"="insurance"]`),
	cat("real_estate", "Real estate agents", "Professional services", "real estate agency", "real_estate_agency", `["office"="estate_agent"]`, `["shop"="estate_agent"]`),
	cat("marketing", "Marketing agencies", "Professional services", "marketing agency", "", `["office"="advertising_agency"]`, `["office"="marketing"]`),
	cat("it", "IT and software", "Professional services", "it services", "", `["office"="it"]`, `["office"="software"]`, `["craft"="computer"]`),
	cat("architect", "Architects", "Professional services", "architect", "", `["office"="architect"]`),
	cat("consulting", "Consultants", "Professional services", "consulting", "", `["office"="consulting"]`),
	cat("financial", "Financial advisors", "Professional services", "financial advisor", "", `["office"="financial_advisor"]`, `["office"="financial"]`),
	cat("coworking", "Coworking spaces", "Professional services", "coworking space", "", `["amenity"="coworking_space"]`, `["office"="coworking"]`),

	cat("car_repair", "Auto repair", "Automotive", "auto repair", "car_repair", `["shop"="car_repair"]`),
	cat("car_dealer", "Car dealers", "Automotive", "car dealer", "car_dealer", `["shop"="car"]`),
	cat("car_wash", "Car washes", "Automotive", "car wash", "car_wash", `["amenity"="car_wash"]`),

	cat("hotel", "Hotels", "Travel and venues", "hotel", "hotel", `["tourism"~"^(hotel|motel|guest_house)$"]`),
	cat("event_venue", "Event venues", "Travel and venues", "event venue", "event_venue", `["amenity"="events_venue"]`, `["amenity"="conference_centre"]`),
	cat("travel_agency", "Travel agencies", "Travel and venues", "travel agency", "travel_agency", `["shop"="travel_agency"]`, `["office"="travel_agent"]`),

	cat("florist", "Florists", "Retail", "florist", "florist", `["shop"="florist"]`),
	cat("furniture", "Furniture stores", "Retail", "furniture store", "furniture_store", `["shop"="furniture"]`),
	cat("hardware", "Hardware stores", "Retail", "hardware store", "hardware_store", `["shop"~"^(hardware|doityourself)$"]`),
	cat("clothing", "Clothing stores", "Retail", "clothing store", "clothing_store", `["shop"~"^(clothes|boutique)$"]`),
	cat("jewelry", "Jewelers", "Retail", "jewelry store", "jewelry_store", `["shop"="jewelry"]`),
	cat("pet_store", "Pet stores", "Retail", "pet store", "pet_store", `["shop"="pet"]`),
	cat("bookstore", "Bookstores", "Retail", "book store", "book_store", `["shop"="books"]`),

	cat("childcare", "Childcare", "Education", "child care", "child_care_agency", `["amenity"~"^(childcare|kindergarten)$"]`),
	cat("driving_school", "Driving schools", "Education", "driving school", "", `["amenity"="driving_school"]`),
	cat("language_school", "Language schools", "Education", "language school", "", `["amenity"="language_school"]`),
	cat("tutoring", "Tutoring", "Education", "tutoring", "", `["amenity"="prep_school"]`, `["office"="tutoring"]`),

	cat("photographer", "Photographers", "Creative", "photographer", "", `["craft"="photographer"]`, `["shop"="photo"]`),
	cat("print_shop", "Print shops", "Creative", "print shop", "", `["shop"="copyshop"]`, `["craft"="printer"]`),
}

// genericOSM are the keys a named business can carry in OpenStreetMap, for a
// text search without a category.
var genericOSM = []string{`["shop"]`, `["amenity"]`, `["office"]`, `["craft"]`, `["tourism"]`, `["healthcare"]`, `["leisure"]`}

// CategoryByID returns the preset with the id, or nil.
func CategoryByID(id string) *Category {
	for i := range Categories {
		if Categories[i].ID == id {
			return &Categories[i]
		}
	}
	return nil
}
