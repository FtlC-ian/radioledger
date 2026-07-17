export interface ApiResponse<T> {
  success: boolean
  message: string
  data: T
  error: string
}

export interface Qso {
  uuid: string
  logbook_uuid?: string
  datetime_on?: string
  callsign: string
  band: string
  mode: string
  frequency?: number | null
  rst_sent?: string | null
  rst_rcvd?: string | null
  gridsquare?: string | null
  name?: string | null
  qth?: string | null
  country?: string | null
  dxcc?: number | null
  cq_zone?: number | null
  itu_zone?: number | null
  continent?: string | null
  comment?: string | null
  notes?: string | null
  created_at?: string
  updated_at?: string
  [key: string]: unknown
}

export interface QsoPayload {
  datetime_on?: string
  qso_datetime?: string
  callsign: string
  band: string
  mode: string
  frequency?: number | null
  rst_sent?: string | null
  rst_rcvd?: string | null
  grid?: string | null
  gridsquare?: string | null
  country?: string | null
  dxcc?: number | null
  power?: number | null
  comment?: string | null
  notes?: string | null
}

export interface QsoSearchFilters {
  callsign?: string
  band?: string
  mode?: string
  dateFrom?: string
  dateTo?: string
}

export interface CursorPage<T> {
  items: T[]
  nextCursor: string | null
  hasMore: boolean
}

export interface CountryStat {
  name: string
  count: number
}

export interface LogbookStats {
  total_qsos: number
  unique_callsigns: number
  unique_countries: number
  unique_grids: number
  bands: Record<string, number>
  modes: Record<string, number>
  top_countries: CountryStat[]
  qsos_by_year: Record<string, number>
  first_qso?: string
  last_qso?: string
}
