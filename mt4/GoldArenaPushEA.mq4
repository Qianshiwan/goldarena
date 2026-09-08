//+------------------------------------------------------------------+
//|                                              GoldArenaPushEA.mq4 |
//|  Pushes the live XAUUSD bid/ask from your MT4 terminal to the    |
//|  GoldArena backend (https://www.kinguizi.top), so the simulation |
//|  uses your real broker feed as the highest-priority price source. |
//|                                                                |
//|  SETUP (do this once in MT4):                                   |
//|   1. Attach this EA to an XAUUSD (or XAUUSDm) chart.            |
//|   2. Tools -> Options -> Expert Advisors                        |
//|        - tick "Allow automated trading"                         |
//|        - tick "Allow WebRequest for listed URL(s)"              |
//|        - add:  https://www.kinguizi.top                         |
//|   3. Allow the EA to trade (it does NOT place orders — WebRequest|
//|      still requires the "automated trading" permission).        |
//|   4. The EA pushes once per second while the market is alive.   |
//+------------------------------------------------------------------+
#property strict

// ---- Edit only if you changed the backend token ----
#define PUSH_URL "https://www.kinguizi.top/api/v1/market/push?token=GAmt4Push_8Kx2qL9vRtZ"
// ------------------------------------------------

#define PUSH_INTERVAL 1          // seconds between pushes (matches backend cadence)
#define SYMBOL_TO_PUSH "XAU"     // backend symbol (always XAU:SPOT)

int OnInit()
{
   Print("GoldArenaPushEA started on ", _Symbol, " -> ", PUSH_URL);
   return(INIT_SUCCEEDED);
}

void OnDeinit(const int reason) {}

void OnTick()
{
   static datetime lastPush = 0;
   if(TimeCurrent() - lastPush < PUSH_INTERVAL)
      return;
   lastPush = TimeCurrent();

   double bid = Bid;
   double ask = Ask;
   if(bid <= 0 || ask <= 0)
      return;

   double price = (bid + ask) / 2.0;

   // Build JSON manually (MQL4 has no native JSON serializer).
   string data = "{\"symbol\":\"" + SYMBOL_TO_PUSH + "\""
               + ",\"price\":" + DoubleToString(price, 2)
               + ",\"bid\":"   + DoubleToString(bid, 2)
               + ",\"ask\":"   + DoubleToString(ask, 2)
               + "}";

   string headers = "Content-Type: application/json\r\n";
   // WebRequest 10-arg signature works on every MT4 build
   // (method, url, headers, timeout, data, cookie, referer, result[], result_headers, response_code).
   string cookie = "";
   string referer = "";
   char resp_bytes[];
   string resp_headers = "";
   int response_code = 0;
   int ret = WebRequest("POST", PUSH_URL, headers, 5000, data,
                        cookie, referer, resp_bytes, resp_headers, response_code);

   static int errCount = 0;
   if(ret != 200)
   {
      if(errCount < 5)
      {
         Print("GoldArenaPushEA: WebRequest failed ret=", ret,
               " (check MT4 Options -> WebRequest whitelist)");
         errCount++;
      }
   }
   else
   {
      errCount = 0; // reset after a success so future errors log again
   }
}
//+------------------------------------------------------------------+
