# Statistics Dashboard

> Explore your operating activity with band/mode breakdowns, QSO totals, activity maps, and more.

## Overview

The Statistics dashboard gives you a bird's-eye view of your operating activity using **Chart.js** visualizations. Access it from **Stats** in the left sidebar or top navigation.

## Available Visualizations

### QSO Activity over Time

View your operating activity as a trend line by day, month, or year. This chart helps identify peak operating periods.

### QSOs by Band and Mode

Bar charts showing a breakdown of your logbook by band and mode (SSB, CW, FT8, FT4, RTTY, etc.). This visualization makes it easy to see where your logbook is most concentrated.

### Operating Pattern Heatmap

An activity heatmap showing which UTC hours you're most active. This is useful for identifying your operating habits and when you're most likely to make contacts.

### Top Callsigns and Countries

Leaderboards showing:
- **Top Callsigns**: The stations you've worked most frequently.
- **Top Countries**: The DXCC entities with the highest QSO counts in your logbook.

### Countries over Time

A cumulative count of DXCC entities worked over the life of your logbook.

## Date Range Filters

All statistics support date range filtering:
- **All time**
- **This year**
- **Last year**
- **Last 30 days**
- **Custom range**

Filtering by date range updates all charts and tables on the dashboard in real time.

## Advanced Analytics

### Activity by Period

Detailed breakdown of QSOs by band, mode, and time period via the `/v1/stats/by-period` endpoint.

### Heatmap Data

Hourly activity distribution via the `/v1/stats/activity-heatmap` endpoint.

## Related

- [Awards Tracking](awards-tracking.md)
- [Search and Filter](search-and-filter.md)
- [DXCC Entities Reference](../reference/dxcc-entities.md)
- [Maidenhead Grids](../reference/maidenhead-grids.md)
