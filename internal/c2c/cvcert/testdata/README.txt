Test fixtures for cvcert are currently synthesized in-process by parser_test.go
(see buildRSACert / buildECCCert). No external binary blobs are committed
because (a) gematik issues real cards via a non-public PKI and we cannot
redistribute customer-card material, and (b) the parser is exercised more
thoroughly by programmatic TLV mutation than by a handful of static blobs.

If a real CV-certificate sample is later sanctioned for redistribution, drop
it here as `egk-real-<role>.cvc.bin` and add a regression test in
parser_test.go that reads it via os.ReadFile.
