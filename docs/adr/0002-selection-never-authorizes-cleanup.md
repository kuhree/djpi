# Selection never participates in the cleanup policy

Review lets the operator mark a Media Item as selected or rejected so that
browsing a large Ingest is tractable. A Selection is a view concern only: it
changes what Review shows and nothing else. Ready for Cleanup remains defined
solely by content-verified Delivery at every required Destination against an
unchanged Source Snapshot, exactly as ADR-0001 states.

The tempting alternative was to let rejection satisfy the policy, so that junk
footage could free Source storage without being promoted anywhere. It was
rejected because it makes a single UI click the difference between keeping and
destroying media, and because it puts an operator's momentary judgment on the
same footing as a SHA-256 verification. djpi's safety property is that only
evidence authorizes destruction. Selection is not evidence.

Rejected Media Items are therefore still fully archived and still verified.
Rejection saves the operator's attention, never storage.
