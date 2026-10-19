Starting March 2023, the daily database backup will have all the current prices data and 6+current month of market history.
If you want all of the market history at once, you should import the latest daily backup and then import each month of historical
market history you want to work with, in descending order. 

There might be some ID conflicts around December 2023, importing March 2023 data, since we moved to monthly backups for market 
history in March 2023. If there are conflicts, the best resolve would be to delete the market history data between 2023-03-01 
and 2023-03-31 and then import the monthly export.

Monthly backups are the same as a daily, just done once a month and help to ensure don't lose any of the market history 
due to some process error that wasn't thought of during development as well as give a starting point of data in a database to work
backwards on, importing market history data from the past.
