package version

// Version is passed at build time (e.g. via -ldflags "-X .../internal/version.Version=1.0.0")
var Version string = "dev"
var SponsorEditionTag string = "" // \nSpecial Edition for\nSponsor
var SponsorEditionText string = "" // \nThank you @Sponsor for your 'Softorage - Open Source Sponsor' package purchase. It really helps ongoing open-source development at Softorage.\n