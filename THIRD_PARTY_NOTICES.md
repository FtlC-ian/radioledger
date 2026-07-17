# Third-party notices

RadioLedger source code is distributed under the GNU Affero General Public
License, version 3 or later; see [LICENSE](LICENSE). Dependency licenses are
retained in their respective package metadata and lockfiles. In particular, the
OpenTelemetry Go packages used by `api/` are Apache-2.0, and Vue and Quasar
are MIT-licensed. Their copyright and license notices remain in the upstream
package distributions.

## Derived reference data

The source tree may include reference data derived from public amateur-radio
standards and community-maintained catalogs. Each imported dataset must retain
its upstream source, version/date, and license in the file header or adjacent
README before it is added to a release. Do not add a dataset whose reuse terms
are unknown.

## Maps and geodata

The public web client does not bundle third-party map artwork, boundary data,
or tiles. A deployment that adds any of those assets must include verified
provenance, the complete upstream license notice, and any required attribution
before release.
