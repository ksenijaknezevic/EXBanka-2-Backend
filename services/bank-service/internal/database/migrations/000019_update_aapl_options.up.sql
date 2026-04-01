-- Dodaje options matricu u details_json za AAPL
-- Simulirani podaci: strike cene oko trenutne cene akcije (~$250)
UPDATE core_banking.listing
SET details_json = '{
  "options": [
    {"strike":230,"callBid":24.10,"callAsk":24.50,"callVol":1520,"callOI":8200,"putBid":0.85,"putAsk":1.05,"putVol":430,"putOI":3100},
    {"strike":235,"callBid":19.30,"callAsk":19.70,"callVol":2100,"callOI":11500,"putBid":1.20,"putAsk":1.45,"putVol":680,"putOI":4800},
    {"strike":240,"callBid":14.80,"callAsk":15.20,"callVol":3400,"callOI":15200,"putBid":1.90,"putAsk":2.15,"putVol":920,"putOI":7200},
    {"strike":245,"callBid":10.60,"callAsk":11.00,"callVol":4800,"callOI":19800,"putBid":2.90,"putAsk":3.20,"putVol":1340,"putOI":9600},
    {"strike":250,"callBid":6.90,"callAsk":7.30,"callVol":7200,"callOI":24500,"putBid":4.50,"putAsk":4.80,"putVol":2100,"putOI":13200},
    {"strike":255,"callBid":4.10,"callAsk":4.40,"callVol":5600,"callOI":21000,"putBid":6.80,"putAsk":7.10,"putVol":1600,"putOI":10500},
    {"strike":260,"callBid":2.20,"callAsk":2.50,"callVol":3900,"callOI":16400,"putBid":9.50,"putAsk":9.90,"putVol":1100,"putOI":7800},
    {"strike":265,"callBid":1.05,"callAsk":1.30,"callVol":2400,"callOI":11200,"putBid":13.10,"putAsk":13.60,"putVol":720,"putOI":5200},
    {"strike":270,"callBid":0.45,"callAsk":0.65,"callVol":1200,"callOI":7400,"putBid":17.20,"putAsk":17.70,"putVol":380,"putOI":3100}
  ]
}'
WHERE ticker = 'AAPL';
