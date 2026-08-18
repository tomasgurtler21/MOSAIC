// Package pricing implements the domain.PricingStore port. It owns the
// external YAML pricing configuration file end to end: loading the model
// price table, validating individual entries (reporting invalid entries as
// findings rather than aborting the load), and persisting new or updated
// entries written by the interactive pricing-gap resolution flow. The
// package translates between the human-readable USD-per-million-tokens
// wire format and the internal nanodollar representation defined in
// internal/domain.
package pricing
